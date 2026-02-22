package web

import (
	"veloria/internal/manager"
)

// SearchService performs code searches across extension stores.
type SearchService interface {
	Search(repoType string, term string, params *manager.SearchParams) (*manager.SearchResponse, error)
}

// ReindexService queues ad-hoc re-index tasks.
type ReindexService interface {
	SubmitReindex(repoType, slug string) bool
}

// SourceResolver locates the on-disk source directory for an extension.
type SourceResolver interface {
	ResolveSourceDir(repoType, slug string) (string, error)
}

// StatsProvider gives read-only access to per-type repository statistics.
type StatsProvider interface {
	Stats(repoType string) (total int, indexed int, ok bool)
	IndexStatus(repoType string) map[string]bool
}
