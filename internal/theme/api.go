package theme

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

type ThemeListItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Indexed   bool      `json:"indexed"`
}

func ViewThemeV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("themes are unavailable"))
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest("invalid UUID"))
			return
		}

		var t repo.Theme
		if err := db.First(&t, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				api.WriteJSON(w, api.ErrNotFound("theme not found"))
			} else {
				api.WriteJSON(w, api.ErrInternal("error fetching theme"))
			}
			return
		}

		api.WriteSuccessJSON(w, http.StatusOK, t)
	})
}

func ListThemesV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("themes are unavailable"))
			return
		}
		pagination, err := api.ParsePagination(r)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest(err.Error()))
			return
		}

		var total int64
		if err := db.Table("themes").Where("deleted_at IS NULL").Count(&total).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error counting themes"))
			return
		}

		var items []ThemeListItem
		if err := db.Table("themes").
			Select("id, name, slug, version, updated_at").
			Where("deleted_at IS NULL").
			Order("updated_at DESC").
			Order("slug ASC").
			Limit(pagination.Limit).
			Offset(pagination.Offset).
			Scan(&items).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error fetching themes"))
			return
		}

		indexedBySlug := map[string]bool{}
		if m := reg.Manager(); m != nil {
			if src := m.GetSource(repo.TypeThemes); src != nil {
				indexedBySlug = src.IndexStatus()
			}
		}
		for i := range items {
			items[i].Indexed = indexedBySlug[items[i].Slug]
		}

		resp := api.ListResponse[ThemeListItem]{
			Page:    pagination.Page,
			PerPage: pagination.PerPage,
			Total:   total,
			Results: items,
		}

		api.WriteSuccessJSON(w, http.StatusOK, resp)
	})
}
