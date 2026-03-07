package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

	"veloria/internal/telemetry"
)

const (
	defaultLimit = 25
	maxLimit     = 100
	maxContext   = 5
)

// NewMCPServer creates a configured MCP server with all tools, resources, and prompts registered.
func NewMCPServer(name, version string, svc SearchService) *server.MCPServer {
	s := server.NewMCPServer(name, version,
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(false, false),
		server.WithPromptCapabilities(false),
		server.WithRecovery(),
	)

	s.AddTools(
		searchCodeTool(svc),
		listExtensionsTool(svc),
		getExtensionDetailsTool(svc),
		getRepoStatsTool(svc),
		listFilesTool(svc),
		readFileTool(svc),
		grepFileTool(svc),
	)

	registerResources(s, svc)
	registerPrompts(s)

	return s
}

func searchCodeTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("search_code",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Search WordPress extension source code (plugins, themes, or core releases). "+
			"Returns a summary of matches on the first call. The summary includes a search_id — pass it "+
			"with offset/limit to paginate through detailed match results without re-running the search. "+
			"Useful for security research (e.g. searching for SQL injection, XSS, or unsafe function patterns), "+
			"vulnerability auditing, and troubleshooting plugin or theme issues."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Search term (regex supported)"),
		),
		mcp.WithString("repo",
			mcp.Description("Repository to search: plugins, themes, or cores"),
			mcp.Enum("plugins", "themes", "cores"),
			mcp.DefaultString("plugins"),
		),
		mcp.WithString("search_id",
			mcp.Description("ID from a previous search — use with offset/limit to paginate results without re-searching"),
		),
		mcp.WithString("file_match",
			mcp.Description("Regex to include only matching filenames (e.g. \"\\.php$\")"),
		),
		mcp.WithString("exclude_file_match",
			mcp.Description("Regex to exclude matching filenames"),
		),
		mcp.WithBoolean("case_sensitive",
			mcp.Description("Enable case-sensitive search"),
			mcp.DefaultBool(false),
		),
		mcp.WithNumber("context_lines",
			mcp.Description("Lines of context before and after each match (0-5)"),
			mcp.DefaultNumber(0),
			mcp.Min(0),
			mcp.Max(float64(maxContext)),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of matches to return per page (1-100)"),
			mcp.DefaultNumber(float64(defaultLimit)),
			mcp.Min(1),
			mcp.Max(float64(maxLimit)),
		),
		mcp.WithNumber("offset",
			mcp.Description("Offset for paginating through match results"),
			mcp.DefaultNumber(0),
			mcp.Min(0),
		),
	)

	return server.ServerTool{
		Tool:    tool,
		Handler: instrumentedHandler("search_code", handleSearchCode(svc)),
	}
}

func handleSearchCode(svc SearchService) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := clampInt(request.GetInt("limit", defaultLimit), 1, maxLimit)
		offset := clampInt(request.GetInt("offset", 0), 0, maxLimit*1000)
		contextLines := clampInt(request.GetInt("context_lines", 0), 0, maxContext)

		// If search_id is provided, load cached results instead of re-searching.
		if searchID := request.GetString("search_id", ""); searchID != "" {
			resp, err := svc.LoadSearch(ctx, searchID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to load search: %s", err)), nil
			}
			text := FormatSearchMatches(resp, offset, limit)
			return mcp.NewToolResultText(text), nil
		}

		query := request.GetString("query", "")
		if query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}

		repo := request.GetString("repo", "plugins")
		if !isValidRepo(repo) {
			return mcp.NewToolResultError("repo must be one of: plugins, themes, cores"), nil
		}

		params := SearchParams{
			Query:            query,
			Repo:             repo,
			FileMatch:        request.GetString("file_match", ""),
			ExcludeFileMatch: request.GetString("exclude_file_match", ""),
			CaseSensitive:    request.GetBool("case_sensitive", false),
			ContextLines:     contextLines,
			Limit:            limit,
			Offset:           offset,
		}

		searchID, resp, err := svc.Search(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %s", err)), nil
		}

		var text string
		if offset == 0 {
			text = FormatSearchSummary(resp, searchID, query, repo)
		} else {
			text = FormatSearchMatches(resp, offset, limit)
		}

		return mcp.NewToolResultText(text), nil
	}
}

func listExtensionsTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("list_extensions",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("List available WordPress extensions (plugins, themes, or core releases). "+
			"Use this to discover valid slugs before searching."),
		mcp.WithString("repo",
			mcp.Description("Repository type: plugins, themes, or cores"),
			mcp.Enum("plugins", "themes", "cores"),
			mcp.DefaultString("plugins"),
		),
		mcp.WithString("search",
			mcp.Description("Filter extensions by name or slug substring"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Results per page (1-100)"),
			mcp.DefaultNumber(float64(defaultLimit)),
			mcp.Min(1),
			mcp.Max(float64(maxLimit)),
		),
		mcp.WithNumber("offset",
			mcp.Description("Offset for pagination"),
			mcp.DefaultNumber(0),
			mcp.Min(0),
		),
	)

	return server.ServerTool{
		Tool:    tool,
		Handler: instrumentedHandler("list_extensions", handleListExtensions(svc)),
	}
}

func handleListExtensions(svc SearchService) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repo := request.GetString("repo", "plugins")
		if !isValidRepo(repo) {
			return mcp.NewToolResultError("repo must be one of: plugins, themes, cores"), nil
		}

		limit := clampInt(request.GetInt("limit", defaultLimit), 1, maxLimit)
		offset := clampInt(request.GetInt("offset", 0), 0, maxLimit*1000)

		params := ListParams{
			Repo:   repo,
			Search: request.GetString("search", ""),
			Limit:  limit,
			Offset: offset,
		}

		resp, err := svc.ListExtensions(ctx, params)
		if err != nil {
			return mcp.NewToolResultError("failed to list extensions, please try again"), nil
		}

		text := FormatExtensionList(resp, repo, offset)
		return mcp.NewToolResultText(text), nil
	}
}

func getExtensionDetailsTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("get_extension_details",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Get detailed metadata for a specific WordPress extension (plugin, theme, or core release). "+
			"Returns version, description, requirements, ratings, install counts, and index status."),
		mcp.WithString("repo",
			mcp.Required(),
			mcp.Description("Repository type: plugins, themes, or cores"),
			mcp.Enum("plugins", "themes", "cores"),
		),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Extension slug (or version number for cores, e.g. \"6.7.1\")"),
		),
	)

	return server.ServerTool{
		Tool: tool,
		Handler: instrumentedHandler("get_extension_details", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			repo := request.GetString("repo", "")
			slug := request.GetString("slug", "")
			if repo == "" || slug == "" {
				return mcp.NewToolResultError("repo and slug are required"), nil
			}

			details, err := svc.GetExtensionDetails(ctx, repo, slug)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(FormatExtensionDetails(details)), nil
		}),
	}
}

func getRepoStatsTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("get_repo_stats",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Get index statistics for WordPress extension repositories. "+
			"Shows total extensions, indexed count, and coverage percentage. "+
			"Omit repo to get stats for all repository types."),
		mcp.WithString("repo",
			mcp.Description("Repository type: plugins, themes, or cores. Omit for all."),
			mcp.Enum("plugins", "themes", "cores"),
		),
	)

	return server.ServerTool{
		Tool: tool,
		Handler: instrumentedHandler("get_repo_stats", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			repo := request.GetString("repo", "")

			stats, err := svc.GetRepoStats(ctx, repo)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(FormatRepoStats(stats)), nil
		}),
	}
}

func listFilesTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("list_files",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("List files in a WordPress extension's source tree. "+
			"Requires the extension to be indexed. Use an optional glob pattern to filter by filename."),
		mcp.WithString("repo",
			mcp.Required(),
			mcp.Description("Repository type: plugins, themes, or cores"),
			mcp.Enum("plugins", "themes", "cores"),
		),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Extension slug (or version number for cores)"),
		),
		mcp.WithString("pattern",
			mcp.Description("Glob pattern to filter filenames (e.g. \"*.php\", \"*.js\"). Matches against the base filename only."),
		),
	)

	return server.ServerTool{
		Tool: tool,
		Handler: instrumentedHandler("list_files", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			repo := request.GetString("repo", "")
			slug := request.GetString("slug", "")
			if repo == "" || slug == "" {
				return mcp.NewToolResultError("repo and slug are required"), nil
			}

			pattern := request.GetString("pattern", "")

			resp, err := svc.ListFiles(ctx, repo, slug, pattern)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(FormatFileList(resp)), nil
		}),
	}
}

func readFileTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("read_file",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Read the contents of a file from a WordPress extension's source tree. "+
			"Requires the extension to be indexed. Returns numbered lines for easy reference. "+
			"Use start_line and max_lines to read specific sections of large files."),
		mcp.WithString("repo",
			mcp.Required(),
			mcp.Description("Repository type: plugins, themes, or cores"),
			mcp.Enum("plugins", "themes", "cores"),
		),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Extension slug (or version number for cores)"),
		),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("File path within the extension (e.g. \"includes/class-wc.php\")"),
		),
		mcp.WithNumber("start_line",
			mcp.Description("Line number to start reading from (default: 1)"),
			mcp.DefaultNumber(1),
			mcp.Min(1),
		),
		mcp.WithNumber("max_lines",
			mcp.Description("Maximum number of lines to return (default: 500, max: 500)"),
			mcp.DefaultNumber(float64(maxReadLines)),
			mcp.Min(1),
			mcp.Max(float64(maxReadLines)),
		),
	)

	return server.ServerTool{
		Tool: tool,
		Handler: instrumentedHandler("read_file", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			repo := request.GetString("repo", "")
			slug := request.GetString("slug", "")
			path := request.GetString("path", "")
			if repo == "" || slug == "" || path == "" {
				return mcp.NewToolResultError("repo, slug, and path are required"), nil
			}

			startLine := clampInt(request.GetInt("start_line", 1), 1, maxReadLines*1000)
			maxLines := clampInt(request.GetInt("max_lines", maxReadLines), 1, maxReadLines)

			resp, err := svc.ReadFile(ctx, repo, slug, path, startLine, maxLines)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(FormatReadFile(resp)), nil
		}),
	}
}

// instrumentedHandler wraps a tool handler to record MCP tool use count and duration.
func instrumentedHandler(toolName string, h server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		result, err := h(ctx, request)
		elapsed := time.Since(start).Seconds()

		attrs := otelmetric.WithAttributes(attribute.String("tool", toolName))
		telemetry.MCPToolUseCount.Add(ctx, 1, attrs)
		telemetry.MCPToolUseDuration.Record(ctx, elapsed, attrs)

		return result, err
	}
}

func grepFileTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("grep_file",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Search within a single extension's source files using regex. "+
			"Bypasses the cross-repo trigram search engine for fast, targeted searches. "+
			"Useful when you already know which plugin or theme to audit."),
		mcp.WithString("repo",
			mcp.Required(),
			mcp.Description("Repository type: plugins, themes, or cores"),
			mcp.Enum("plugins", "themes", "cores"),
		),
		mcp.WithString("slug",
			mcp.Required(),
			mcp.Description("Extension slug (or version number for cores)"),
		),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Regex pattern to search for"),
		),
		mcp.WithString("file_match",
			mcp.Description("Glob pattern to filter files (e.g. \"*.php\", \"*.js\"). Matches against the base filename only."),
		),
		mcp.WithBoolean("case_sensitive",
			mcp.Description("Enable case-sensitive search"),
			mcp.DefaultBool(false),
		),
		mcp.WithNumber("context_lines",
			mcp.Description("Lines of context before and after each match (0-5)"),
			mcp.DefaultNumber(0),
			mcp.Min(0),
			mcp.Max(float64(maxContext)),
		),
	)

	return server.ServerTool{
		Tool:    tool,
		Handler: instrumentedHandler("grep_file", handleGrepFile(svc)),
	}
}

func handleGrepFile(svc SearchService) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		repo := request.GetString("repo", "")
		slug := request.GetString("slug", "")
		query := request.GetString("query", "")
		if repo == "" || slug == "" || query == "" {
			return mcp.NewToolResultError("repo, slug, and query are required"), nil
		}

		if !isValidRepo(repo) {
			return mcp.NewToolResultError("repo must be one of: plugins, themes, cores"), nil
		}

		params := GrepFileParams{
			Repo:          repo,
			Slug:          slug,
			Query:         query,
			FileMatch:     request.GetString("file_match", ""),
			CaseSensitive: request.GetBool("case_sensitive", false),
			ContextLines:  clampInt(request.GetInt("context_lines", 0), 0, maxContext),
		}

		resp, err := svc.GrepFile(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		return mcp.NewToolResultText(FormatGrepFile(resp)), nil
	}
}

// registerResources adds MCP resources to the server.
func registerResources(s *server.MCPServer, svc SearchService) {
	// Static resource: repository statistics.
	s.AddResource(
		mcp.NewResource("veloria://stats", "Repository Statistics",
			mcp.WithResourceDescription("Current index statistics for all WordPress extension repositories"),
			mcp.WithMIMEType("text/plain"),
		),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			stats, err := svc.GetRepoStats(ctx, "")
			if err != nil {
				return nil, err
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      "veloria://stats",
					MIMEType: "text/plain",
					Text:     FormatRepoStats(stats),
				},
			}, nil
		},
	)

	// Resource template: extension details by repo/slug.
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("veloria://{repo}/{slug}/info", "Extension Details",
			mcp.WithTemplateDescription("Detailed metadata for a specific WordPress extension (plugin, theme, or core release)"),
			mcp.WithTemplateMIMEType("text/plain"),
		),
		func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			repo, slug, err := parseExtensionURI(request.Params.URI)
			if err != nil {
				return nil, err
			}

			details, err := svc.GetExtensionDetails(ctx, repo, slug)
			if err != nil {
				return nil, err
			}

			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      request.Params.URI,
					MIMEType: "text/plain",
					Text:     FormatExtensionDetails(details),
				},
			}, nil
		},
	)
}

// parseExtensionURI extracts repo and slug from a "veloria://{repo}/{slug}/info" URI.
func parseExtensionURI(uri string) (repo, slug string, err error) {
	const prefix = "veloria://"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", fmt.Errorf("invalid URI: %s", uri)
	}
	rest := strings.TrimPrefix(uri, prefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 3 || parts[2] != "info" {
		return "", "", fmt.Errorf("invalid URI format: %s", uri)
	}
	repo = parts[0]
	slug = parts[1]
	if !isValidRepo(repo) || slug == "" {
		return "", "", fmt.Errorf("invalid repo or slug in URI: %s", uri)
	}
	return repo, slug, nil
}

// clampInt constrains v to [min, max].
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
