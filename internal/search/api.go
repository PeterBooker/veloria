package search

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"

	api "veloria/internal/api"
	"veloria/internal/manager"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/service"
	"veloria/internal/storage"
	"veloria/internal/telemetry"
	typespb "veloria/internal/types"
	"veloria/internal/web"
)

type SearchRequest struct {
	Term             string `json:"term"`
	Repo             string `json:"repo"`
	FileMatch        string `json:"file_match,omitempty"`
	ExcludeFileMatch string `json:"exclude_file_match,omitempty"`
	CaseSensitive    bool   `json:"case_sensitive,omitempty"`
	Public           *bool  `json:"public,omitempty"`
}

func ViewSearchV1(reg *service.Registry) http.Handler {
	return api.Handler[uuid.UUID, searchmodel.Search]{
		Decode: func(r *http.Request) (uuid.UUID, error) {
			if reg.DB() == nil {
				return uuid.Nil, api.ErrUnavailable("searches are unavailable")
			}
			return api.DecodeIDParam(r, "id")
		},
		Endpoint: func(ctx context.Context, id uuid.UUID) (searchmodel.Search, error) {
			db := reg.DB()
			var s searchmodel.Search
			if err := db.First(&s, "id = ?", id).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					return s, api.ErrNotFound("search not found")
				}
				return s, api.ErrInternal("error fetching search")
			}

			if s.Status == searchmodel.StatusCompleted {
				if s3 := reg.S3(); s3 != nil {
					sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
					defer cancel()

					var protoResults typespb.SearchResponse
					if err := s3.DownloadResult(sctx, s.ID.String(), &protoResults); err == nil {
						s.Results = searchmodel.SearchResponseFromProto(&protoResults)
					}
				}
			}

			return s, nil
		},
	}
}

func CreateSearchV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		m := reg.Manager()
		s3 := reg.S3()
		if db == nil || m == nil || s3 == nil {
			api.WriteError(w, api.ErrUnavailable("search is temporarily unavailable"))
			return
		}

		req, err := api.DecodeJSON[SearchRequest](r)
		if err != nil {
			api.WriteError(w, err)
			return
		}

		if req.Term == "" {
			api.WriteError(w, api.ErrBadRequest("term is a required field"))
			return
		}

		if req.Repo == "" {
			req.Repo = "plugins"
		}

		switch req.Repo {
		case "plugins", "themes", "cores":
		default:
			api.WriteError(w, api.ErrBadRequest("repo must be one of: plugins, themes, cores"))
			return
		}

		private := false
		if req.Public != nil {
			private = !*req.Public
		}
		if !private && shouldForcePrivate(req.Term) {
			private = true
		}

		s := searchmodel.Search{
			Status:  searchmodel.StatusQueued,
			Private: private,
			Term:    req.Term,
			Repo:    req.Repo,
		}
		if err := db.Create(&s).Error; err != nil {
			api.WriteError(w, api.ErrInternal("failed to create search record"))
			return
		}

		// Run search asynchronously — the client polls GET /api/v1/search/{id}
		// for results. This avoids HTTP write timeout issues when searches are
		// slow (e.g. during heavy indexing).
		go runAPISearchAsync(db, m, s3, s.ID, req) // #nosec G118 -- goroutine intentionally outlives request; search runs in background

		api.WriteSuccessJSON(w, http.StatusAccepted, s)
	})
}

// runAPISearchAsync executes a search in the background and persists results.
func runAPISearchAsync(db *gorm.DB, m *manager.Manager, s3 storage.ResultStorage, searchID uuid.UUID, req SearchRequest) {
	db.Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusProcessing)

	searchStart := time.Now()
	results, err := m.Search(req.Repo, req.Term, &manager.SearchParams{
		FileMatch:        req.FileMatch,
		ExcludeFileMatch: req.ExcludeFileMatch,
		CaseInsensitive:  !req.CaseSensitive,
	})
	searchElapsed := time.Since(searchStart).Seconds()

	repoAttr := attribute.String("repo", req.Repo)
	telemetry.SearchCount.Add(context.Background(), 1, metric.WithAttributes(repoAttr))
	telemetry.SearchDuration.Record(context.Background(), searchElapsed, metric.WithAttributes(repoAttr))

	if err != nil {
		db.Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusFailed)
		return
	}

	now := time.Now()
	totalMatches := web.CountTotalMatches(results)
	persistSearchResults(db, s3, searchID, results, now, totalMatches, results.Total)
}

// persistSearchResults uploads search results to S3 and updates the DB record.
func persistSearchResults(db *gorm.DB, s3 storage.ResultStorage, searchID uuid.UUID, results *manager.SearchResponse, completedAt time.Time, totalMatches int, totalExtensions int) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	protoResults := searchmodel.SearchResponseToProto(results)
	size, err := s3.UploadResult(ctx, searchID.String(), protoResults)
	if err != nil {
		db.Model(&searchmodel.Search{}).Where("id = ?", searchID).Update("status", searchmodel.StatusFailed)
		return
	}

	db.Model(&searchmodel.Search{}).Where("id = ?", searchID).Updates(map[string]any{
		"status":           searchmodel.StatusCompleted,
		"results_size":     size,
		"completed_at":     completedAt,
		"total_matches":    totalMatches,
		"total_extensions": totalExtensions,
	})
}

type SearchListItem struct {
	ID          uuid.UUID  `json:"id"`
	Status      string     `json:"status"`
	Private     bool       `json:"private"`
	Term        string     `json:"term"`
	Repo        string     `json:"repo"`
	ResultsSize *int64     `json:"results_size,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	UserID      *uuid.UUID `json:"user_id,omitempty"`
}

func ListSearchesV1(reg *service.Registry) http.Handler {
	return api.ListHandler[SearchListItem](reg, api.ListConfig[SearchListItem]{
		EntityName:    "searches",
		Table:         "searches",
		SelectColumns: "id, status, private, term, repo, results_size, completed_at, created_at, updated_at, user_id",
		WhereClause:   "deleted_at IS NULL AND private = false",
		OrderClauses:  []string{"created_at DESC"},
	})
}
