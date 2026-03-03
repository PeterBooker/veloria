package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerPrompts adds all prompt definitions to the MCP server.
func registerPrompts(s *server.MCPServer) {
	s.AddPrompt(checkCompatibilityPrompt())
	s.AddPrompt(pluginOverviewPrompt())
	s.AddPrompt(themeOverviewPrompt())
}

func checkCompatibilityPrompt() (mcp.Prompt, server.PromptHandlerFunc) {
	prompt := mcp.NewPrompt("check-compatibility",
		mcp.WithPromptDescription("Check WordPress and PHP compatibility for an extension"),
		mcp.WithArgument("slug",
			mcp.RequiredArgument(),
			mcp.ArgumentDescription("Extension slug to check"),
		),
		mcp.WithArgument("repo",
			mcp.ArgumentDescription("Repository type: plugins, themes, or cores (default: plugins)"),
		),
	)

	handler := func(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		slug := request.Params.Arguments["slug"]
		if slug == "" {
			return nil, fmt.Errorf("slug is required")
		}
		repo := request.Params.Arguments["repo"]
		if repo == "" {
			repo = "plugins"
		}

		text := fmt.Sprintf(`Check the WordPress and PHP compatibility for the %s "%s":

1. Use get_extension_details with repo=%q and slug=%q to retrieve version requirements (Requires WP, Tested up to, Requires PHP).

2. Use grep_file with repo=%q and slug=%q to search for deprecated WordPress function calls. Look for patterns like:
   - "mysql_query" (deprecated since WP 3.5)
   - "ereg\(" or "eregi\(" (removed in PHP 8.0)
   - "create_function" (deprecated in PHP 7.2)
   - "each\(" (deprecated in PHP 7.2, removed in 8.0)
   - "get_magic_quotes" (removed in PHP 8.0)

3. Use grep_file with repo=%q and slug=%q to search for PHP version-specific syntax that may indicate actual minimum PHP requirements beyond what the metadata declares:
   - "match\(" (PHP 8.0 match expression)
   - "\?->" (PHP 8.0 nullsafe operator)
   - "enum " (PHP 8.1 enums)
   - "readonly " (PHP 8.1 readonly properties)
   - "fn(" (PHP 7.4 arrow functions)
   - Typed properties (PHP 7.4)

4. Summarize the compatibility status:
   - WordPress version range (requires / tested up to)
   - Declared PHP version requirement vs. actual syntax observed
   - Any deprecated function usage found
   - Overall compatibility assessment and recommendations`, repo, slug, repo, slug, repo, slug, repo, slug)

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Compatibility check for %s/%s", repo, slug),
			Messages: []mcp.PromptMessage{
				{Role: mcp.RoleUser, Content: mcp.NewTextContent(text)},
			},
		}, nil
	}

	return prompt, handler
}

func pluginOverviewPrompt() (mcp.Prompt, server.PromptHandlerFunc) {
	prompt := mcp.NewPrompt("plugin-overview",
		mcp.WithPromptDescription("Get a comprehensive overview of a WordPress plugin's architecture"),
		mcp.WithArgument("slug",
			mcp.RequiredArgument(),
			mcp.ArgumentDescription("Plugin slug to analyze"),
		),
	)

	handler := func(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		slug := request.Params.Arguments["slug"]
		if slug == "" {
			return nil, fmt.Errorf("slug is required")
		}

		text := fmt.Sprintf(`Provide a comprehensive architectural overview of the WordPress plugin "%s":

1. Use get_extension_details with repo="plugins" and slug=%q to get the plugin's metadata, version, and description.

2. Use list_files with repo="plugins" and slug=%q to see the full file structure. Identify the directory layout and organizational patterns.

3. Use read_file to read the main plugin file (typically %s.php or the file containing the "Plugin Name:" header). Look for the plugin bootstrap, main class instantiation, and hook registrations.

4. Identify key architectural components:
   - Main entry point and initialization flow
   - Class autoloading or file inclusion patterns
   - WordPress hooks registered (actions and filters)
   - Custom post types, taxonomies, or REST API endpoints
   - Admin interface components vs. frontend components
   - Third-party library dependencies

5. Summarize your findings:
   - Plugin purpose and core functionality
   - Architecture pattern (procedural, OOP, MVC, etc.)
   - Directory structure and file organization
   - Key classes and their responsibilities
   - Hook integration points with WordPress
   - Notable implementation patterns or concerns`, slug, slug, slug, slug)

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Architecture overview for plugin %s", slug),
			Messages: []mcp.PromptMessage{
				{Role: mcp.RoleUser, Content: mcp.NewTextContent(text)},
			},
		}, nil
	}

	return prompt, handler
}

func themeOverviewPrompt() (mcp.Prompt, server.PromptHandlerFunc) {
	prompt := mcp.NewPrompt("theme-overview",
		mcp.WithPromptDescription("Get a comprehensive overview of a WordPress theme's architecture"),
		mcp.WithArgument("slug",
			mcp.RequiredArgument(),
			mcp.ArgumentDescription("Theme slug to analyze"),
		),
	)

	handler := func(_ context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		slug := request.Params.Arguments["slug"]
		if slug == "" {
			return nil, fmt.Errorf("slug is required")
		}

		text := fmt.Sprintf(`Provide a comprehensive architectural overview of the WordPress theme "%s":

1. Use get_extension_details with repo="themes" and slug=%q to get the theme's metadata, version, and requirements.

2. Use list_files with repo="themes" and slug=%q to see the full file structure. Identify the template hierarchy and organizational patterns.

3. Use read_file to examine key theme files:
   - style.css — theme metadata header, CSS architecture
   - functions.php — theme setup, feature support, enqueued assets, hook registrations
   - index.php — base template fallback

4. Identify key architectural components:
   - Template hierarchy usage (single.php, archive.php, page.php, etc.)
   - Template parts and reusable components (header.php, footer.php, sidebar.php, template-parts/)
   - Theme customizer integration (customize_register hooks)
   - Block theme support (theme.json, block templates, block patterns)
   - Navigation menus, widget areas, and sidebars
   - Asset loading strategy (CSS, JavaScript)

5. Summarize your findings:
   - Theme purpose and design approach
   - Classic theme vs. block theme (or hybrid)
   - Template structure and hierarchy coverage
   - Customization options available
   - Key functions and hook registrations
   - Notable patterns, third-party dependencies, or concerns`, slug, slug, slug)

		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Architecture overview for theme %s", slug),
			Messages: []mcp.PromptMessage{
				{Role: mcp.RoleUser, Content: mcp.NewTextContent(text)},
			},
		}, nil
	}

	return prompt, handler
}
