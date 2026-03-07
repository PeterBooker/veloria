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
	TotalMatches    int               `json:"total_matches"`
	TotalExtensions int               `json:"total_extensions"`
	Extensions      []ExtensionResult `json:"extensions"`
}

// ExtensionResult holds search results for a single extension.
type ExtensionResult struct {
	Slug           string        `json:"slug"`
	Name           string        `json:"name"`
	Version        string        `json:"version"`
	ActiveInstalls int           `json:"active_installs"`
	TotalMatches   int           `json:"total_matches"`
	Matches        []MatchDetail `json:"matches,omitempty"`
}

// MatchDetail holds a single code match.
type MatchDetail struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Content string   `json:"content"`
	Before  []string `json:"before,omitempty"`
	After   []string `json:"after,omitempty"`
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

// ExtensionDetails holds full metadata for a single extension.
type ExtensionDetails struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Source           string `json:"source"`
	ShortDescription string `json:"short_description"`
	Requires         string `json:"requires"`
	Tested           string `json:"tested"`
	RequiresPHP      string `json:"requires_php"`
	Rating           int    `json:"rating"`
	ActiveInstalls   int    `json:"active_installs"`
	Downloaded       int    `json:"downloaded"`
	DownloadLink     string `json:"download_link"`
	Indexed          bool   `json:"indexed"`
	FileCount        int    `json:"file_count"`
	TotalSize        int64  `json:"total_size"`
}

// RepoStats holds index statistics for a single repository type.
type RepoStats struct {
	Repo    string `json:"repo"`
	Total   int    `json:"total"`
	Indexed int    `json:"indexed"`
}

// FileEntry represents a single file in an extension's source tree.
type FileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ListFilesResponse holds the file listing for an extension.
type ListFilesResponse struct {
	Slug  string      `json:"slug"`
	Repo  string      `json:"repo"`
	Total int         `json:"total"`
	Files []FileEntry `json:"files"`
}

// ReadFileResponse holds the contents of a file from an extension.
type ReadFileResponse struct {
	Slug       string `json:"slug"`
	Repo       string `json:"repo"`
	Path       string `json:"path"`
	TotalLines int    `json:"total_lines"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Content    string `json:"content"`
}

// GrepFileParams holds the parameters for a grep_file request.
type GrepFileParams struct {
	Repo          string
	Slug          string
	Query         string
	FileMatch     string
	CaseSensitive bool
	ContextLines  int
}

// GrepFileResponse holds the results of a grep_file operation.
type GrepFileResponse struct {
	Slug         string          `json:"slug"`
	Repo         string          `json:"repo"`
	Query        string          `json:"query"`
	TotalMatches int             `json:"total_matches"`
	Files        []GrepFileMatch `json:"files"`
}

// GrepFileMatch holds matches within a single file.
type GrepFileMatch struct {
	Path    string          `json:"path"`
	Matches []GrepLineMatch `json:"matches"`
}

// GrepLineMatch holds a single line match with optional context.
type GrepLineMatch struct {
	Line    int      `json:"line"`
	Content string   `json:"content"`
	Before  []string `json:"before,omitempty"`
	After   []string `json:"after,omitempty"`
}
