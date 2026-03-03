package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestCheckCompatibilityPrompt(t *testing.T) {
	prompt, handler := checkCompatibilityPrompt()

	if prompt.Name != "check-compatibility" {
		t.Errorf("name = %q, want %q", prompt.Name, "check-compatibility")
	}

	result, err := handler(context.Background(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Name:      "check-compatibility",
			Arguments: map[string]string{"slug": "woocommerce"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(result.Messages))
	}
	if result.Messages[0].Role != mcp.RoleUser {
		t.Errorf("role = %q, want %q", result.Messages[0].Role, mcp.RoleUser)
	}

	text := result.Messages[0].Content.(mcp.TextContent).Text
	if !strings.Contains(text, "woocommerce") {
		t.Errorf("should contain slug, got:\n%s", text)
	}
	if !strings.Contains(text, "get_extension_details") {
		t.Errorf("should reference get_extension_details tool, got:\n%s", text)
	}
}

func TestCheckCompatibilityPrompt_WithRepo(t *testing.T) {
	_, handler := checkCompatibilityPrompt()

	result, err := handler(context.Background(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Arguments: map[string]string{"slug": "twentytwentyfour", "repo": "themes"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Messages[0].Content.(mcp.TextContent).Text
	if !strings.Contains(text, "twentytwentyfour") {
		t.Errorf("should contain slug, got:\n%s", text)
	}
	if !strings.Contains(text, `repo="themes"`) {
		t.Errorf("should use themes repo, got:\n%s", text)
	}
}

func TestCheckCompatibilityPrompt_MissingSlug(t *testing.T) {
	_, handler := checkCompatibilityPrompt()

	_, err := handler(context.Background(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Arguments: map[string]string{},
		},
	})
	if err == nil {
		t.Error("expected error for missing slug")
	}
}

func TestPluginOverviewPrompt(t *testing.T) {
	prompt, handler := pluginOverviewPrompt()

	if prompt.Name != "plugin-overview" {
		t.Errorf("name = %q, want %q", prompt.Name, "plugin-overview")
	}

	result, err := handler(context.Background(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Arguments: map[string]string{"slug": "jetpack"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Messages[0].Content.(mcp.TextContent).Text
	if !strings.Contains(text, "jetpack") {
		t.Errorf("should contain slug, got:\n%s", text)
	}
	if !strings.Contains(text, "list_files") {
		t.Errorf("should reference list_files tool, got:\n%s", text)
	}
	if !strings.Contains(text, "read_file") {
		t.Errorf("should reference read_file tool, got:\n%s", text)
	}
}

func TestThemeOverviewPrompt(t *testing.T) {
	prompt, handler := themeOverviewPrompt()

	if prompt.Name != "theme-overview" {
		t.Errorf("name = %q, want %q", prompt.Name, "theme-overview")
	}

	result, err := handler(context.Background(), mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Arguments: map[string]string{"slug": "twentytwentyfour"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	text := result.Messages[0].Content.(mcp.TextContent).Text
	if !strings.Contains(text, "twentytwentyfour") {
		t.Errorf("should contain slug, got:\n%s", text)
	}
	if !strings.Contains(text, "style.css") {
		t.Errorf("should reference style.css, got:\n%s", text)
	}
	if !strings.Contains(text, "functions.php") {
		t.Errorf("should reference functions.php, got:\n%s", text)
	}
	if !strings.Contains(text, "theme.json") {
		t.Errorf("should reference theme.json for block theme detection, got:\n%s", text)
	}
}
