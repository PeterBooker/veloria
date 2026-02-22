package web

import (
	"net/http"

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
	Templates            *Templates
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
func NewDeps(templates *Templates, db *gorm.DB, search SearchService, reindex ReindexService, sources SourceResolver, stats StatsProvider, s3 storage.ResultStorage, ch cache.Cache, cfg *config.Config, searchEnabled bool, searchDisabledReason string) *Deps {
	return &Deps{
		Templates:            templates,
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
	return PageData{
		User:                 auth.UserFromContext(r.Context()),
		SearchEnabled:        d.SearchEnabled,
		SearchDisabledReason: d.SearchDisabledReason,
		CurrentPath:          r.URL.Path,
		Version:              config.Version,
	}
}

// SearchAvailable returns whether the search feature is fully operational.
func (d *Deps) SearchAvailable() bool {
	return d.SearchEnabled && d.DB != nil && d.Search != nil && d.S3 != nil
}
