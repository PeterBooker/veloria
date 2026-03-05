package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearch(t *testing.T) {
	// Get the working directory and construct the test index path
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Navigate up to project root and into testdata
	projectRoot := filepath.Dir(filepath.Dir(wd))
	indexPath := filepath.Join(projectRoot, "testdata", "plugins", "index", "wordpress-seo")

	// Check if the test index exists
	if _, err := os.Stat(filepath.Join(indexPath, "trigrams")); os.IsNotExist(err) {
		t.Skipf("test index not found at %s, skipping", indexPath)
	}

	// Open the index
	idx := Open(indexPath)
	if idx == nil {
		t.Fatalf("failed to open index at %s", indexPath)
	}

	// Perform a search
	opt := &SearchOptions{
		IgnoreCase:    true,
		LiteralSearch: false,
		MaxResults:    10,
	}

	resp, err := idx.Search("function", opt)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	t.Logf("Search completed in %v", resp.Duration)
	t.Logf("Files opened: %d", resp.FilesOpened)
	t.Logf("Files with match: %d", resp.FilesWithMatch)
	t.Logf("Total file matches: %d", len(resp.Matches))

	if len(resp.Matches) == 0 {
		t.Error("expected at least one match for 'function' in WordPress SEO plugin")
	}

	// Print some matches for verification
	for i, fm := range resp.Matches {
		if i >= 3 {
			break
		}
		t.Logf("File: %s (%d matches)", fm.Filename, len(fm.Matches))
		for j, m := range fm.Matches {
			if j >= 2 {
				break
			}
			t.Logf("  Line %d: %s", m.LineNumber, truncate(m.Line, 80))
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
