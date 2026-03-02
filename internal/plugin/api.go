package plugin

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

type PluginListItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Indexed   bool      `json:"indexed"`
}

func ViewPluginV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("plugins are unavailable"))
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest("invalid UUID"))
			return
		}

		var p repo.Plugin
		if err := db.First(&p, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				api.WriteJSON(w, api.ErrNotFound("plugin not found"))
			} else {
				api.WriteJSON(w, api.ErrInternal("error fetching plugin"))
			}
			return
		}

		api.WriteSuccessJSON(w, http.StatusOK, p)
	})
}

func ListPluginsV1(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		db := reg.DB()
		if db == nil {
			api.WriteJSON(w, api.ErrUnavailable("plugins are unavailable"))
			return
		}
		pagination, err := api.ParsePagination(r)
		if err != nil {
			api.WriteJSON(w, api.ErrBadRequest(err.Error()))
			return
		}

		var total int64
		if err := db.Table("plugins").Where("deleted_at IS NULL").Count(&total).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error counting plugins"))
			return
		}

		var items []PluginListItem
		if err := db.Table("plugins").
			Select("id, name, slug, version, updated_at").
			Where("deleted_at IS NULL").
			Order("updated_at DESC").
			Order("slug ASC").
			Limit(pagination.Limit).
			Offset(pagination.Offset).
			Scan(&items).Error; err != nil {
			api.WriteJSON(w, api.ErrInternal("error fetching plugins"))
			return
		}

		indexedBySlug := map[string]bool{}
		if m := reg.Manager(); m != nil {
			if src := m.GetSource(repo.TypePlugins); src != nil {
				indexedBySlug = src.IndexStatus()
			}
		}
		for i := range items {
			items[i].Indexed = indexedBySlug[items[i].Slug]
		}

		resp := api.ListResponse[PluginListItem]{
			Page:    pagination.Page,
			PerPage: pagination.PerPage,
			Total:   total,
			Results: items,
		}

		api.WriteSuccessJSON(w, http.StatusOK, resp)
	})
}
