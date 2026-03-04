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

// DataSourceSummary contains aggregated stats for a data source.
type DataSourceSummary struct {
	Type           string
	Title          string
	Total          int
	Indexed        int
	IndexedPercent float64
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
	View       string // "" (all/public) or "own" (user's own)
	LoggedIn   bool   // whether to show the toggle
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
	SearchID         string
	SearchDataSource string
	Extensions       []ExtensionResultSummary
	Page             int
	TotalPages       int
	Search           string
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
	SearchID         string
	SearchDataSource string
	Result           *manager.SearchResult
}

// SearchContextLine represents a single line of context.
type SearchContextLine struct {
	Number  int
	Text    string
	IsMatch bool
}

// SearchContextData contains data for the match context modal.
type SearchContextData struct {
	DataSource string
	Slug       string
	Filename   string
	Lines      []SearchContextLine
	Error      string
}

// DataSourcesData contains data for the data sources listing page.
type DataSourcesData struct {
	PageData
	DataSourceSummaries []DataSourceSummary
}

// DataSourceItem represents an item in a data source list.
type DataSourceItem struct {
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

// FailedIndexEvent represents a failed indexing attempt for display.
type FailedIndexEvent struct {
	Slug         string
	ErrorMessage string
	CreatedAt    time.Time
}

// FailedIndexData contains data for the failed indexing HTMX partial.
type FailedIndexData struct {
	DataSource string
	Events     []FailedIndexEvent
	Page       int
	TotalPages int
	TotalCount int
}

// DataSourceData contains data for a single data source view.
type DataSourceData struct {
	PageData
	DataSourceSummary  DataSourceSummary
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

// LargestDataSourceFile represents a large file with its parent extension info.
type LargestDataSourceFile struct {
	Path string
	Size int64
	Slug string
	Name string
}

// DataSourceItemsData contains data for the data source items HTMX partial.
type DataSourceItemsData struct {
	DataSource string
	Items      []DataSourceItem
	Page       int
	TotalPages int
	Search     string
}

// ReportedSearchItem contains data for one reported search in the admin view.
type ReportedSearchItem struct {
	ReportID         string
	SearchID         string
	SearchTerm       string
	SearchDataSource string
	SearchPrivate    bool
	ReporterName     string
	Reason           string
	ReportedAt       string
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
	DataSourceType   string
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
