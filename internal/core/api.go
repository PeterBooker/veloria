package core

import (
	"net/http"
	"time"

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

func ViewCoreV1(reg *service.Registry) http.Handler {
	return api.ViewByID[repo.Core](reg, "core", "id", func(db *gorm.DB, id uuid.UUID) (repo.Core, error) {
		var c repo.Core
		err := db.First(&c, "id = ?", id).Error
		return c, err
	})
}

func ListCoresV1(reg *service.Registry) http.Handler {
	return api.ListHandler[CoreListItem](reg, api.ListConfig[CoreListItem]{
		EntityName:    "cores",
		Table:         "cores",
		SelectColumns: "id, name, version AS slug, version, updated_at",
		WhereClause:   "deleted_at IS NULL",
		OrderClauses:  []string{"updated_at DESC", "version DESC"},
		Enrich:        enrichCoreIndex,
	})
}

func enrichCoreIndex(reg *service.Registry, items []CoreListItem) {
	if m := reg.Manager(); m != nil {
		if src := m.GetSource(repo.TypeCores); src != nil {
			indexed := src.IndexStatus()
			for i := range items {
				items[i].Indexed = indexed[items[i].Version]
			}
		}
	}
}
