package core

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	api "veloria/internal/api"
	"veloria/internal/repo"
	"veloria/internal/service"
)

type CoreListItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Indexed   bool      `json:"indexed"`
}

type coreRow struct {
	ID        uuid.UUID
	Name      string
	Version   string
	UpdatedAt time.Time
}

func ViewCoreV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("cores are unavailable"))
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest("invalid UUID"))
			return
		}

		var c repo.Core
		if err := db.First(&c, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				api.WriteJSON(w, api.ErrNotFound("core not found"))
			} else {
				api.WriteJSON(w, api.ErrInternal("error fetching core"))
			}
			return
		}

		api.WriteSuccessJSON(w, http.StatusOK, c)
	})
}

func ListCoresV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("cores are unavailable"))
			return
		}
		pagination, err := api.ParsePagination(r)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest(err.Error()))
			return
		}

		var total int64
		if err := db.Table("cores").Where("deleted_at IS NULL").Count(&total).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error counting cores"))
			return
		}

		var rows []coreRow
		if err := db.Table("cores").
			Select("id, name, version, updated_at").
			Where("deleted_at IS NULL").
			Order("updated_at DESC").
			Order("version DESC").
			Limit(pagination.Limit).
			Offset(pagination.Offset).
			Scan(&rows).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error fetching cores"))
			return
		}

		indexedBySlug := map[string]bool{}
		if m := reg.Manager(); m != nil {
			if src := m.GetSource(repo.TypeCores); src != nil {
				indexedBySlug = src.IndexStatus()
			}
		}

		items := make([]CoreListItem, len(rows))
		for i, row := range rows {
			items[i] = CoreListItem{
				ID:        row.ID,
				Name:      row.Name,
				Slug:      row.Version,
				Version:   row.Version,
				UpdatedAt: row.UpdatedAt,
				Indexed:   indexedBySlug[row.Version],
			}
		}

		resp := api.ListResponse[CoreListItem]{
			Page:    pagination.Page,
			PerPage: pagination.PerPage,
			Total:   total,
			Results: items,
		}

		api.WriteSuccessJSON(w, http.StatusOK, resp)
	})
}
