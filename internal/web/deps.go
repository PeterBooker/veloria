package web

import (
	"net/http"

	"gorm.io/gorm"

	"veloria/internal/auth"
	"veloria/internal/cache"
	"veloria/internal/config"
	"veloria/internal/manager"
	"veloria/internal/storage"
)

// Deps holds shared dependencies for all web handlers.
type Deps struct {
	Templates            *Templates
	DB                   *gorm.DB
	Manager              *manager.Manager
	S3                   storage.ResultStorage
	Cache                cache.Cache
	Config               *config.Config
	SearchEnabled        bool
	SearchDisabledReason string
}

// NewDeps creates a new shared dependency container for web handlers.
func NewDeps(templates *Templates, db *gorm.DB, m *manager.Manager, s3 storage.ResultStorage, ch cache.Cache, cfg *config.Config, searchEnabled bool, searchDisabledReason string) *Deps {
	return &Deps{
		Templates:            templates,
		DB:                   db,
		Manager:              m,
		S3:                   s3,
		Cache:                ch,
		Config:               cfg,
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
	}
}

// SearchAvailable returns whether the search feature is fully operational.
func (d *Deps) SearchAvailable() bool {
	return d.SearchEnabled && d.DB != nil && d.Manager != nil && d.S3 != nil
}
