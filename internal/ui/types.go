package ui

import (
	"time"

	"github.com/google/uuid"

	"veloria/internal/manager"
	searchmodel "veloria/internal/search/model"
	"veloria/internal/user"
)

// OGMeta holds Open Graph and Twitter Card meta tag values for a page.
type OGMeta struct {
	Title       string // og:title
	Description string // og:description
	URL         string // og:url — canonical URL
	Image       string // og:image — absolute URL to OG image
	Type        string // og:type — "website" or "article"
}

// PageData contains common data for all pages.
type PageData struct {
	User                 *user.User
	EnabledProviders     []string
	SearchEnabled        bool
	SearchDisabledReason string
	CurrentPath          string
	Version              string
	OG                   OGMeta
	RequestStart         time.Time
}

// LoginData contains data for the login page.
type LoginData struct {
	PageData
	Error string
}

// SearchSummary contains metadata for listing searches.
type SearchSummary struct {
	Search       searchmodel.Search
	MatchCount   int
	MatchesKnown bool
}

// RepoSummary contains aggregated stats for a repository.
type RepoSummary struct {
	Repo           string
	Title          string
	Total          int
	Indexed        int
	IndexedPercent int
}

// HomeData contains data for the home page template.
type HomeData struct {
	PageData
	RecentSearches []SearchSummary
}

// SearchesData contains data for the searches page template.
type SearchesData struct {
	PageData
	Searches   []SearchSummary
	Page       int
	TotalPages int
}

// MySearchesData contains data for the my searches page template.
type MySearchesData struct {
	PageData
	Searches   []SearchSummary
	Page       int
	TotalPages int
}

// SearchResultsData contains data for search results partial.
type SearchResultsData struct {
	PageData
	Search  searchmodel.Search
	Results *manager.SearchResponse
	Error   string
}

// SearchViewData contains data for the single search view.
type SearchViewData struct {
	PageData
	Search           searchmodel.Search
	TotalMatches     int
	TotalExtensions  int
	DurationMs       int64
	ProgressSearched int
	ProgressTotal    int
	Error            string
}

// SearchExtensionsData contains data for the search extensions HTMX partial.
type SearchExtensionsData struct {
	SearchID   string
	SearchRepo string
	Extensions []ExtensionResultSummary
	Page       int
	TotalPages int
	Search     string
}

// ExtensionResultSummary contains summary info for one extension in search results (no match details).
type ExtensionResultSummary struct {
	Slug           string
	Name           string
	Version        string
	ActiveInstalls int
	TotalMatches   int
}

// ExtensionResultsData contains data for rendering a single extension's detailed matches.
type ExtensionResultsData struct {
	SearchID   string
	SearchRepo string
	Result     *manager.SearchResult
}

// SearchContextLine represents a single line of context.
type SearchContextLine struct {
	Number  int
	Text    string
	IsMatch bool
}

// SearchContextData contains data for the match context modal.
type SearchContextData struct {
	Repo     string
	Slug     string
	Filename string
	Lines    []SearchContextLine
	Error    string
}

// ReposData contains data for the repos listing page.
type ReposData struct {
	PageData
	RepoSummaries []RepoSummary
}

// RepoItem represents an item in a repository list.
type RepoItem struct {
	ID         uuid.UUID
	Name       string
	Slug       string
	Version    string
	Indexed    bool
	Downloaded int
	FileCount  int
	TotalSize  int64
}

// ChartData holds JSON-encoded chart values for client-side rendering.
type ChartData struct {
	JSON string // JSON array of numbers, e.g. "[10,20,30]"
	Max  int64
}

// LargestExtension represents an extension ranked by total download size.
type LargestExtension struct {
	Slug      string
	Name      string
	TotalSize int64
	FileCount int
}

// RepoData contains data for a single repository view.
type RepoData struct {
	PageData
	RepoSummary        RepoSummary
	ActiveInstalls     ChartData
	FileCount          ChartData
	FileSize           ChartData
	LargestBySize      []LargestExtension
	LargestByFileCount []LargestExtension
}

// FileStat represents a file with its size for display.
type FileStat struct {
	Path string
	Size int64
}

// LargestRepoFile represents a large file with its parent extension info.
type LargestRepoFile struct {
	Path string
	Size int64
	Slug string
	Name string
}

// RepoItemsData contains data for the repo items HTMX partial.
type RepoItemsData struct {
	Repo       string
	Items      []RepoItem
	Page       int
	TotalPages int
	Search     string
}

// ReportedSearchItem contains data for one reported search in the admin view.
type ReportedSearchItem struct {
	ReportID      string
	SearchID      string
	SearchTerm    string
	SearchRepo    string
	SearchPrivate bool
	ReporterName  string
	Reason        string
	ReportedAt    string
}

// ReportsPageData contains data for the admin reports page.
type ReportsPageData struct {
	PageData
	Reports    []ReportedSearchItem
	Page       int
	TotalPages int
}

// VisibilityToggleData contains data for the admin visibility toggle partial.
type VisibilityToggleData struct {
	SearchID string
	Private  bool
}

// ExtensionData contains data for single extension views.
type ExtensionData struct {
	PageData
	RepoType         string
	Name             string
	Slug             string
	Source           string
	Version          string
	ShortDescription string
	Author           string
	Requires         string
	Tested           string
	RequiresPHP      string
	Rating           int
	ActiveInstalls   int
	Downloaded       int
	DownloadLink     string
	Tags             map[string]string
	Indexed          bool
	FileCount        int
	TotalSize        int64
	LargestFiles     []FileStat
}
