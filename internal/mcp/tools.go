package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultLimit = 25
	maxLimit     = 100
	maxContext   = 5
)

// NewMCPServer creates a configured MCP server with all tools registered.
func NewMCPServer(name, version string, svc SearchService) *server.MCPServer {
	s := server.NewMCPServer(name, version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	s.AddTools(
		searchCodeTool(svc),
		listExtensionsTool(svc),
	)

	return s
}

func searchCodeTool(svc SearchService) server.ServerTool {
	tool := mcp.NewTool("search_code",
		mcp.WithDescription("Search WordPress extension source code (plugins, themes, or core releases). "+
			"Returns a summary of matches on the first call. The summary includes a search_id — pass it "+
			"with offset/limit to paginate through detailed match results without re-running the search."),
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
		Handler: handleSearchCode(svc),
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
			return mcp.NewToolResultError("search failed, please try again"), nil
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
		Handler: handleListExtensions(svc),
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
