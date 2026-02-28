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

func TestFormatExtensionDetails(t *testing.T) {
	d := &ExtensionDetails{
		Slug:             "woocommerce",
		Name:             "WooCommerce",
		Version:          "8.5.0",
		Source:           "wordpress.org",
		ShortDescription: "An eCommerce toolkit.",
		Requires:         "6.4",
		Tested:           "6.7",
		RequiresPHP:      "7.4",
		Rating:           88,
		ActiveInstalls:   5000000,
		Downloaded:       300000000,
		Indexed:          true,
		FileCount:        1234,
		TotalSize:        5242880,
	}

	text := FormatExtensionDetails(d)

	checks := []string{
		"WooCommerce (woocommerce) v8.5.0",
		"An eCommerce toolkit.",
		"Source:          wordpress.org",
		"Active installs: 5,000,000",
		"Downloads:       300,000,000",
		"Rating:          88/100",
		"Requires WP:     6.4",
		"Tested up to:    6.7",
		"Requires PHP:    7.4",
		"Index status:    indexed",
		"Files:           1,234",
		"Total size:      5.0 MB",
	}
	for _, want := range checks {
		if !strings.Contains(text, want) {
			t.Errorf("should contain %q, got:\n%s", want, text)
		}
	}
}

func TestFormatExtensionDetails_Minimal(t *testing.T) {
	d := &ExtensionDetails{
		Slug:    "6.7.1",
		Name:    "WordPress 6.7.1",
		Version: "6.7.1",
		Source:  "wordpress.org",
		Indexed: false,
	}

	text := FormatExtensionDetails(d)

	if !strings.Contains(text, "Index status:    not indexed") {
		t.Errorf("should show not indexed, got:\n%s", text)
	}
	// Should not show fields that are zero/empty
	if strings.Contains(text, "Active installs") {
		t.Errorf("should not show zero active installs, got:\n%s", text)
	}
	if strings.Contains(text, "Files:") {
		t.Errorf("should not show file count when not indexed, got:\n%s", text)
	}
}

func TestFormatRepoStats(t *testing.T) {
	stats := []RepoStats{
		{Repo: "plugins", Total: 60000, Indexed: 54000},
		{Repo: "themes", Total: 12000, Indexed: 10800},
		{Repo: "cores", Total: 80, Indexed: 80},
	}

	text := FormatRepoStats(stats)

	if !strings.Contains(text, "Repository Statistics") {
		t.Errorf("should have header, got:\n%s", text)
	}
	if !strings.Contains(text, "plugins") {
		t.Errorf("should list plugins, got:\n%s", text)
	}
	if !strings.Contains(text, "60,000 total") {
		t.Errorf("should format totals, got:\n%s", text)
	}
	if !strings.Contains(text, "90.0%") {
		t.Errorf("should show percentage, got:\n%s", text)
	}
	if !strings.Contains(text, "100.0%") {
		t.Errorf("should show 100%% for cores, got:\n%s", text)
	}
}

func TestFormatFileList(t *testing.T) {
	resp := &ListFilesResponse{
		Slug:  "woocommerce",
		Repo:  "plugins",
		Total: 3,
		Files: []FileEntry{
			{Path: "woocommerce.php", Size: 12345},
			{Path: "includes/class-wc.php", Size: 67890},
			{Path: "readme.txt", Size: 500},
		},
	}

	text := FormatFileList(resp)

	if !strings.Contains(text, "plugins/woocommerce: 3 files") {
		t.Errorf("should show slug and count, got:\n%s", text)
	}
	if !strings.Contains(text, "woocommerce.php") {
		t.Errorf("should list files, got:\n%s", text)
	}
	if !strings.Contains(text, "12.1 KB") {
		t.Errorf("should format sizes, got:\n%s", text)
	}
}

func TestFormatFileList_Empty(t *testing.T) {
	resp := &ListFilesResponse{
		Slug: "empty", Repo: "plugins", Total: 0,
	}

	text := FormatFileList(resp)
	if !strings.Contains(text, "0 files") {
		t.Errorf("should show 0 files, got:\n%s", text)
	}
}

func TestFormatReadFile(t *testing.T) {
	resp := &ReadFileResponse{
		Slug:       "woocommerce",
		Repo:       "plugins",
		Path:       "woocommerce.php",
		TotalLines: 100,
		StartLine:  1,
		EndLine:    3,
		Content:    " 1  <?php\n 2  // WooCommerce\n 3  defined('ABSPATH') || exit;\n",
	}

	text := FormatReadFile(resp)

	if !strings.Contains(text, "plugins/woocommerce") {
		t.Errorf("should show repo/slug, got:\n%s", text)
	}
	if !strings.Contains(text, "woocommerce.php (100 lines total)") {
		t.Errorf("should show total lines, got:\n%s", text)
	}
	if !strings.Contains(text, "Showing lines 1–3") {
		t.Errorf("should show line range, got:\n%s", text)
	}
	if !strings.Contains(text, "<?php") {
		t.Errorf("should contain file content, got:\n%s", text)
	}
	if !strings.Contains(text, "start_line=4") {
		t.Errorf("should show pagination hint, got:\n%s", text)
	}
}

func TestFormatReadFile_Complete(t *testing.T) {
	resp := &ReadFileResponse{
		Slug:       "tiny",
		Repo:       "plugins",
		Path:       "tiny.php",
		TotalLines: 2,
		StartLine:  1,
		EndLine:    2,
		Content:    "1  <?php\n2  echo 'hi';\n",
	}

	text := FormatReadFile(resp)

	if strings.Contains(text, "More lines available") {
		t.Errorf("should not show pagination hint when complete, got:\n%s", text)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{5242880, "5.0 MB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.expected)
		}
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
