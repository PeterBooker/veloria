package web

import (
	"net/http"
	"time"

	"github.com/a-h/templ"
	"gorm.io/gorm"

	"veloria/internal/auth"
	"veloria/internal/cache"
	"veloria/internal/config"
	"veloria/internal/service"
	"veloria/internal/storage"
)

// Deps holds shared dependencies for all web handlers.
// Manager capabilities are exposed through narrow interfaces (SearchService,
// ReindexService, SourceResolver, StatsProvider) defined in interfaces.go.
// Service availability is resolved dynamically via the Registry so that
// handlers see reconnected services without requiring a restart.
type Deps struct {
	Registry *service.Registry
	Cache    cache.Cache
	Config   *config.Config
	Progress *ProgressStore
}

// NewDeps creates a new shared dependency container for web handlers.
func NewDeps(reg *service.Registry, ch cache.Cache, cfg *config.Config) *Deps {
	return &Deps{
		Registry: reg,
		Cache:    ch,
		Config:   cfg,
		Progress: &ProgressStore{},
	}
}

// PageData returns common data for all page templates.
func (d *Deps) PageData(r *http.Request) PageData {
	appURL := d.Config.AppURL
	canonicalURL := appURL + r.URL.Path

	return PageData{
		User:                 auth.UserFromContext(r.Context()),
		SearchEnabled:        d.Registry.SearchEnabled(),
		SearchDisabledReason: d.Registry.SearchDisabledReason(),
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
	return d.Registry.SearchEnabled()
}

// DB returns the current database connection, or nil if unavailable.
func (d *Deps) DB() *gorm.DB { return d.Registry.DB() }

// S3 returns the current result storage, or nil if unavailable.
func (d *Deps) S3() storage.ResultStorage { return d.Registry.S3() }

// Search returns the search service, or nil if the manager is unavailable.
// The explicit nil check avoids returning a non-nil interface with a nil value.
func (d *Deps) Search() SearchService {
	if m := d.Registry.Manager(); m != nil {
		return m
	}
	return nil
}

// Reindex returns the reindex service, or nil if the manager is unavailable.
func (d *Deps) Reindex() ReindexService {
	if m := d.Registry.Manager(); m != nil {
		return m
	}
	return nil
}

// Sources returns the source resolver, or nil if the manager is unavailable.
func (d *Deps) Sources() SourceResolver {
	if m := d.Registry.Manager(); m != nil {
		return m
	}
	return nil
}

// Stats returns the stats provider, or nil if the manager is unavailable.
func (d *Deps) Stats() StatsProvider {
	if m := d.Registry.Manager(); m != nil {
		return m
	}
	return nil
}
