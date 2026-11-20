package index

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildTestIndex indexes n small PHP files that each contain "needle" once,
// laid out the way production stores an extension, and returns the open index
// with its source directory.
func buildTestIndex(t *testing.T, n int) (*Index, string) {
	t.Helper()

	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "repo", "source", "example")
	indexDir := filepath.Join(dir, "repo", "index", "example")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		body := fmt.Sprintf("<?php\nfunction needle_%d() {\n\treturn %d;\n}\n", i, i)
		if err := os.WriteFile(filepath.Join(sourceDir, fmt.Sprintf("file%02d.php", i)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	IndexDirToFile(sourceDir, filepath.Join(indexDir, "trigrams"), "")

	idx := Open(indexDir)
	if idx == nil {
		t.Fatal("failed to open built index")
	}
	t.Cleanup(idx.Close)
	return idx, sourceDir
}

// stepContext reports itself cancelled once Err has been called more than
// allowed times, which makes "cancelled while scanning" deterministic.
type stepContext struct {
	context.Context
	allowed int
	calls   int
}

func (c *stepContext) Err() error {
	c.calls++
	if c.calls > c.allowed {
		return context.Canceled
	}
	return nil
}

// The search deadline must stop a scan between files, not only between
// extensions: once the context is done no further file is visited.
func TestSearchCompiledStopsWhenContextDone(t *testing.T) {
	const files = 20
	idx, sourceDir := buildTestIndex(t, files)

	cs, err := CompileSearch("needle", &SearchOptions{LiteralSearch: true, MaxResults: 1000})
	if err != nil {
		t.Fatal(err)
	}

	// Control: a live context scans every file.
	resp, err := idx.SearchCompiled(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	if resp.FilesOpened != files || resp.FilesWithMatch != files {
		t.Fatalf("control search opened %d files and matched %d, want %d of each", resp.FilesOpened, resp.FilesWithMatch, files)
	}

	// Cancelled after three files: the fourth check ends the scan.
	const allowed = 3
	ctx := &stepContext{Context: context.Background(), allowed: allowed}
	resp, err = idx.SearchCompiled(ctx, cs)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-scan cancel: got error %v, want context.Canceled (the scan ran on after its context was done)", err)
	}
	if resp != nil {
		t.Fatalf("mid-scan cancel: got a response with %d files opened, want none", resp.FilesOpened)
	}
	if ctx.calls != allowed+1 {
		t.Fatalf("mid-scan cancel: context checked %d times, want %d (the scan must stop at the first failed check)", ctx.calls, allowed+1)
	}

	// Cancelled before the scan: no file is read at all. With the sources
	// gone, a scan that still visited files would skip each unreadable one
	// and report success.
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err = idx.SearchCompiled(cancelled, cs)
	if !errors.Is(err, context.Canceled) || resp != nil {
		t.Fatalf("cancelled before scan: got error %v (response nil: %t), want context.Canceled and no response", err, resp == nil)
	}
}
