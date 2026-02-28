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

var searchSem = make(chan struct{}, 1)

func acquireSearchSlot(ctx context.Context) error {
	select {
	case searchSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseSearchSlot() {
	<-searchSem
}

func ViewSearchV1(db *gorm.DB, s3 storage.ResultStorage) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		if s.Status == searchmodel.StatusCompleted && s3 != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()

			var protoResults typespb.SearchResponse
			if err := s3.DownloadResult(ctx, s.ID.String(), &protoResults); err == nil {
				s.Results = searchmodel.SearchResponseFromProto(&protoResults)
			}
		}

		api.WriteSuccessJSON(w, http.StatusOK, s)
	})
}

func CreateSearchV1(db *gorm.DB, m manager.Searcher, s3 storage.ResultStorage) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		if err := acquireSearchSlot(r.Context()); err != nil {
			api.WriteJSON(w, api.ErrTimeout("request cancelled"))
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
			releaseSearchSlot()
			api.WriteJSON(w, api.ErrInternal("failed to create search record"))
			return
		}

		db.Model(&s).Update("status", searchmodel.StatusProcessing)
		s.Status = searchmodel.StatusProcessing

		searchStart := time.Now()
		results, err := m.Search(req.Repo, req.Term, &manager.SearchParams{
			FileMatch:        req.FileMatch,
			ExcludeFileMatch: req.ExcludeFileMatch,
			CaseInsensitive:  !req.CaseSensitive,
		})
		searchElapsed := time.Since(searchStart).Seconds()

		// Search done — free the slot immediately so the next search can start.
		releaseSearchSlot()

		repoAttr := attribute.String("repo", req.Repo)
		telemetry.SearchCount.Add(r.Context(), 1, metric.WithAttributes(repoAttr))
		telemetry.SearchDuration.Record(r.Context(), searchElapsed, metric.WithAttributes(repoAttr))

		if err != nil {
			db.Model(&s).Update("status", searchmodel.StatusFailed)
			api.WriteJSON(w, api.ErrInternal("search failed"))
			return
		}

		now := time.Now()
		totalMatches := web.CountTotalMatches(results)

		s.Status = searchmodel.StatusCompleted
		s.CompletedAt = &now
		s.TotalMatches = &totalMatches
		s.TotalExtensions = &results.Total
		s.Results = results

		// Persist results to S3 and update DB in the background so the
		// client gets its response without waiting for the network upload.
		go persistSearchResults(db, s3, s.ID, results, now, totalMatches, results.Total) // #nosec G118 -- goroutine intentionally outlives request; S3 upload runs in background

		api.WriteSuccessJSON(w, http.StatusCreated, s)
	})
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

func ListSearchesV1(db *gorm.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
