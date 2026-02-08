package manager

// Searcher is the interface used by API search handlers to perform searches.
type Searcher interface {
	Search(repoType string, term string, params *SearchParams) (*SearchResponse, error)
}

// RepoStatsProvider gives read-only access to repository statistics.
// Used by API list handlers to determine index status.
type RepoStatsProvider interface {
	Stats() (total int, indexed int)
	IndexStatus() map[string]bool
}
