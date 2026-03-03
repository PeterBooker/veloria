package search

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("searches are unavailable"))
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := api.ParseID(idStr)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest("invalid search id"))
			return
		}

		var s searchmodel.Search
		if err := db.First(&s, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				api.WriteJSON(w, api.ErrNotFound("search not found"))
			} else {
				api.WriteJSON(w, api.ErrInternal("error fetching search"))
			}
			return
		}

		if s.Status == searchmodel.StatusCompleted {
			if s3 := reg.S3(); s3 != nil {
				ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				defer cancel()

				var protoResults typespb.SearchResponse
				if err := s3.DownloadResult(ctx, s.ID.String(), &protoResults); err == nil {
					s.Results = searchmodel.SearchResponseFromProto(&protoResults)
				}
			}
		}

		api.WriteSuccessJSON(w, http.StatusOK, s)
	})
}

func CreateSearchV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		m := reg.Manager()
		s3 := reg.S3()
		if db == nil || m == nil || s3 == nil {
			api.WriteJSON(w, api.ErrUnavailable("search is temporarily unavailable"))
			return
		}
		var req SearchRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			api.WriteJSON(w, api.ErrBadRequest("failed to decode JSON body"))
			return
		}

		if req.Term == "" {
			api.WriteJSON(w, api.ErrBadRequest("term is a required field"))
			return
		}

		if req.Repo == "" {
			req.Repo = "plugins"
		}

		switch req.Repo {
		case "plugins", "themes", "cores":
		default:
			api.WriteJSON(w, api.ErrBadRequest("repo must be one of: plugins, themes, cores"))
			return
		}

		private := false
		if req.Public != nil {
			private = !*req.Public
		}

		s := searchmodel.Search{
			Status:  searchmodel.StatusQueued,
			Private: private,
			Term:    req.Term,
			Repo:    req.Repo,
		}
		if err := db.Create(&s).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("failed to create search record"))
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
// It runs in a background goroutine so the API response is not blocked by S3 I/O.
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("searches are unavailable"))
			return
		}
		pagination, err := api.ParsePagination(r)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest(err.Error()))
			return
		}

		var total int64
		if err := db.Table("searches").Where("deleted_at IS NULL AND private = false").Count(&total).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error counting searches"))
			return
		}

		var items []SearchListItem
		if err := db.Table("searches").
			Select("id, status, private, term, repo, results_size, completed_at, created_at, updated_at, user_id").
			Where("deleted_at IS NULL AND private = false").
			Order("created_at DESC").
			Limit(pagination.Limit).
			Offset(pagination.Offset).
			Scan(&items).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error fetching searches"))
			return
		}

		resp := api.ListResponse[SearchListItem]{
			Page:    pagination.Page,
			PerPage: pagination.PerPage,
			Total:   total,
			Results: items,
		}

		api.WriteSuccessJSON(w, http.StatusOK, resp)
	})
}
