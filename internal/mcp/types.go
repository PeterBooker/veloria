package mcp

// SearchParams holds the parameters for a code search request.
type SearchParams struct {
	Query            string
	Repo             string
	FileMatch        string
	ExcludeFileMatch string
	CaseSensitive    bool
	ContextLines     int
	Limit            int
	Offset           int
}

// SearchResponse holds the results of a code search.
type SearchResponse struct {
	TotalMatches     int                `json:"total_matches"`
	TotalExtensions  int                `json:"total_extensions"`
	Extensions       []ExtensionResult  `json:"extensions"`
}

// ExtensionResult holds search results for a single extension.
type ExtensionResult struct {
	Slug           string       `json:"slug"`
	Name           string       `json:"name"`
	Version        string       `json:"version"`
	ActiveInstalls int          `json:"active_installs"`
	TotalMatches   int          `json:"total_matches"`
	Matches        []MatchDetail `json:"matches,omitempty"`
}

// MatchDetail holds a single code match.
type MatchDetail struct {
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Content    string   `json:"content"`
	Before     []string `json:"before,omitempty"`
	After      []string `json:"after,omitempty"`
}

// ListParams holds the parameters for listing extensions.
type ListParams struct {
	Repo   string
	Search string
	Limit  int
	Offset int
}

// ListResponse holds a paginated list of extensions.
type ListResponse struct {
	Total      int                `json:"total"`
	Extensions []ExtensionSummary `json:"extensions"`
}

// ExtensionSummary is a lightweight extension record for listing.
type ExtensionSummary struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Indexed bool   `json:"indexed"`
}
