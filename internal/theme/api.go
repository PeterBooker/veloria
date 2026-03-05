package theme

import (
	"net/http"
	"time"

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
	return api.ViewByID[repo.Theme](reg, "theme", "id", func(db *gorm.DB, id uuid.UUID) (repo.Theme, error) {
		var t repo.Theme
		err := db.First(&t, "id = ?", id).Error
		return t, err
	})
}

func ListThemesV1(reg *service.Registry) http.Handler {
	return api.ListHandler[ThemeListItem](reg, api.ListConfig[ThemeListItem]{
		EntityName:    "themes",
		Table:         "themes",
		SelectColumns: "id, name, slug, version, updated_at",
		WhereClause:   "deleted_at IS NULL",
		OrderClauses:  []string{"updated_at DESC", "slug ASC"},
		Enrich:        enrichThemeIndex,
	})
}

func enrichThemeIndex(reg *service.Registry, items []ThemeListItem) {
	if m := reg.Manager(); m != nil {
		if src := m.GetSource(repo.TypeThemes); src != nil {
			indexed := src.IndexStatus()
			for i := range items {
				items[i].Indexed = indexed[items[i].Slug]
			}
		}
	}
}
