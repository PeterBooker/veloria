package mcp

import (
	"strings"
	"testing"
)

func TestFormatSearchSummary(t *testing.T) {
	resp := &SearchResponse{
		TotalMatches:    42,
		TotalExtensions: 3,
		Extensions: []ExtensionResult{
			{Slug: "woocommerce", Name: "WooCommerce", Version: "8.5.0", ActiveInstalls: 5000000, TotalMatches: 20},
			{Slug: "jetpack", Name: "Jetpack", Version: "12.9", ActiveInstalls: 5000000, TotalMatches: 15},
			{Slug: "my-plugin", Name: "My Plugin", Version: "1.0.0", ActiveInstalls: 0, TotalMatches: 7},
		},
	}

	text := FormatSearchSummary(resp, "abc-123", "add_filter", "plugins")

	if !strings.Contains(text, "42 matches across 3 plugins") {
		t.Errorf("summary should contain match count, got:\n%s", text)
	}
	if !strings.Contains(text, "search_id: abc-123") {
		t.Errorf("summary should contain search_id, got:\n%s", text)
	}
	if !strings.Contains(text, "WooCommerce (woocommerce)") {
		t.Errorf("summary should list extensions, got:\n%s", text)
	}
	if !strings.Contains(text, "5,000,000 active installs") {
		t.Errorf("summary should format active installs, got:\n%s", text)
	}
	// Extension with 0 active installs should not show install count
	if !strings.Contains(text, "My Plugin (my-plugin) v1.0.0 — 7 matches\n") {
		t.Errorf("should not show active installs for extensions with 0 installs, got:\n%s", text)
	}
}

func TestFormatSearchSummary_Empty(t *testing.T) {
	resp := &SearchResponse{}
	text := FormatSearchSummary(resp, "", "nonexistent", "plugins")

	if !strings.Contains(text, "0 matches across 0 plugins") {
		t.Errorf("empty summary should show 0 matches, got:\n%s", text)
	}
	if strings.Contains(text, "search_id") {
		t.Errorf("empty search_id should not appear in output, got:\n%s", text)
	}
}

func TestFormatSearchMatches(t *testing.T) {
	resp := &SearchResponse{
		Extensions: []ExtensionResult{
			{
				Slug: "woo",
				Name: "WooCommerce",
				Matches: []MatchDetail{
					{File: "includes/class-wc.php", Line: 42, Content: "add_filter('init', 'wc_init');"},
					{File: "includes/class-wc.php", Line: 100, Content: "add_filter('admin_init', 'wc_admin');"},
				},
			},
			{
				Slug: "jet",
				Name: "Jetpack",
				Matches: []MatchDetail{
					{File: "jetpack.php", Line: 10, Content: "add_filter('plugins_loaded', 'jp_load');"},
				},
			},
		},
	}

	// Page 1
	text := FormatSearchMatches(resp, 0, 2)
	if !strings.Contains(text, "Matches 1–2 of 3") {
		t.Errorf("should show match range, got:\n%s", text)
	}
	if !strings.Contains(text, "More results available. Use offset=2") {
		t.Errorf("should show pagination hint, got:\n%s", text)
	}

	// Page 2
	text = FormatSearchMatches(resp, 2, 2)
	if !strings.Contains(text, "Matches 3–3 of 3") {
		t.Errorf("should show last page, got:\n%s", text)
	}
	if strings.Contains(text, "More results") {
		t.Errorf("should not show more hint on last page, got:\n%s", text)
	}
}

func TestFormatSearchMatches_WithContext(t *testing.T) {
	resp := &SearchResponse{
		Extensions: []ExtensionResult{
			{
				Slug: "test",
				Name: "Test",
				Matches: []MatchDetail{
					{
						File:    "test.php",
						Line:    5,
						Content: "add_filter('init');",
						Before:  []string{"<?php", "// Setup"},
						After:   []string{"do_action('loaded');"},
					},
				},
			},
		},
	}

	text := FormatSearchMatches(resp, 0, 25)
	if !strings.Contains(text, "| <?php") {
		t.Errorf("should show before context, got:\n%s", text)
	}
	if !strings.Contains(text, "> add_filter") {
		t.Errorf("should show matched line, got:\n%s", text)
	}
	if !strings.Contains(text, "| do_action") {
		t.Errorf("should show after context, got:\n%s", text)
	}
}

func TestFormatSearchMatches_BeyondOffset(t *testing.T) {
	resp := &SearchResponse{
		Extensions: []ExtensionResult{
			{Slug: "test", Name: "Test", Matches: []MatchDetail{
				{File: "a.php", Line: 1, Content: "x"},
			}},
		},
	}

	text := FormatSearchMatches(resp, 100, 25)
	if !strings.Contains(text, "No matches at offset 100") {
		t.Errorf("should indicate no matches at offset, got:\n%s", text)
	}
}

func TestFormatExtensionList(t *testing.T) {
	resp := &ListResponse{
		Total: 50,
		Extensions: []ExtensionSummary{
			{Slug: "woocommerce", Name: "WooCommerce", Version: "8.5.0", Indexed: true},
			{Slug: "jetpack", Name: "Jetpack", Version: "12.9", Indexed: false},
		},
	}

	text := FormatExtensionList(resp, "plugins", 0)
	if !strings.Contains(text, "plugins: 50 total") {
		t.Errorf("should show total, got:\n%s", text)
	}
	if !strings.Contains(text, "woocommerce") {
		t.Errorf("should list extensions, got:\n%s", text)
	}
	if !strings.Contains(text, "[indexed]") {
		t.Errorf("should show indexed status, got:\n%s", text)
	}
	if !strings.Contains(text, "[not indexed]") {
		t.Errorf("should show not indexed status, got:\n%s", text)
	}
	if !strings.Contains(text, "offset=2") {
		t.Errorf("should show pagination hint, got:\n%s", text)
	}
}

func TestFormatExtensionList_Empty(t *testing.T) {
	resp := &ListResponse{Total: 0}
	text := FormatExtensionList(resp, "themes", 0)
	if !strings.Contains(text, "No extensions found") {
		t.Errorf("should show empty message, got:\n%s", text)
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{5000000, "5,000,000"},
		{12345, "12,345"},
	}

	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.expected {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
