package plugin

import (
	"net/http"
	"time"

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
	return api.ViewByID[repo.Plugin](reg, "plugin", "id", func(db *gorm.DB, id uuid.UUID) (repo.Plugin, error) {
		var p repo.Plugin
		err := db.First(&p, "id = ?", id).Error
		return p, err
	})
}

func ListPluginsV1(reg *service.Registry) http.Handler {
	return api.ListHandler[PluginListItem](reg, api.ListConfig[PluginListItem]{
		EntityName:    "plugins",
		Table:         "plugins",
		SelectColumns: "id, name, slug, version, updated_at",
		WhereClause:   "deleted_at IS NULL",
		OrderClauses:  []string{"updated_at DESC", "slug ASC"},
		Enrich:        enrichPluginIndex,
	})
}

func enrichPluginIndex(reg *service.Registry, items []PluginListItem) {
	if m := reg.Manager(); m != nil {
		if src := m.GetSource(repo.TypePlugins); src != nil {
			indexed := src.IndexStatus()
			for i := range items {
				items[i].Indexed = indexed[items[i].Slug]
			}
		}
	}
}
