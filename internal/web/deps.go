package web

import (
	"net/http"
	"time"

	"github.com/a-h/templ"
	"gorm.io/gorm"

	"veloria/internal/auth"
	"veloria/internal/cache"
	"veloria/internal/config"
	"veloria/internal/storage"
)

// Deps holds shared dependencies for all web handlers.
// Manager capabilities are exposed through narrow interfaces (SearchService,
// ReindexService, SourceResolver, StatsProvider) defined in interfaces.go.
type Deps struct {
	DB                   *gorm.DB
	Search               SearchService
	Reindex              ReindexService
	Sources              SourceResolver
	Stats                StatsProvider
	S3                   storage.ResultStorage
	Cache                cache.Cache
	Config               *config.Config
	Progress             *ProgressStore
	SearchEnabled        bool
	SearchDisabledReason string
}

// NewDeps creates a new shared dependency container for web handlers.
// All four interface parameters (search, reindex, sources, stats) may be nil
// when the manager is unavailable (e.g. no database).
func NewDeps(db *gorm.DB, search SearchService, reindex ReindexService, sources SourceResolver, stats StatsProvider, s3 storage.ResultStorage, ch cache.Cache, cfg *config.Config, searchEnabled bool, searchDisabledReason string) *Deps {
	return &Deps{
		DB:                   db,
		Search:               search,
		Reindex:              reindex,
		Sources:              sources,
		Stats:                stats,
		S3:                   s3,
		Cache:                ch,
		Config:               cfg,
		Progress:             &ProgressStore{},
		SearchEnabled:        searchEnabled,
		SearchDisabledReason: searchDisabledReason,
	}
}

// PageData returns common data for all page templates.
func (d *Deps) PageData(r *http.Request) PageData {
	appURL := d.Config.AppURL
	canonicalURL := appURL + r.URL.Path

	return PageData{
		User:                 auth.UserFromContext(r.Context()),
		SearchEnabled:        d.SearchEnabled,
		SearchDisabledReason: d.SearchDisabledReason,
		CurrentPath:          r.URL.Path,
		Version:              config.Version,
		RequestStart:         time.Now(),
		OG: OGMeta{
			Title:       "Veloria - WordPress Code Search",
			Description: "Search through every WordPress plugin, theme, and core version with powerful regex patterns.",
			URL:         canonicalURL,
			Image:       appURL + "/og-default.png",
			Type:        "website",
		},
	}
}

// RenderComponent renders a templ component, setting the Content-Type header.
func (d *Deps) RenderComponent(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// SearchAvailable returns whether the search feature is fully operational.
func (d *Deps) SearchAvailable() bool {
	return d.SearchEnabled && d.DB != nil && d.Search != nil && d.S3 != nil
}
