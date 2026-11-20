package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"veloria/internal/config"
	"veloria/internal/index"
)

// newSearchStore builds a plugin store holding extensions indexed extensions
// of files small PHP files each, laid out the way production stores them
// (DATA_DIR/plugins/{source,index}/<slug>). Every file contains "needle".
func newSearchStore(t *testing.T, extensions, files, concurrency int) *ExtensionStore[*Plugin] {
	t.Helper()

	dataDir := t.TempDir()
	store := NewExtensionStore(StoreConfig[*Plugin]{
		Ctx:           context.Background(),
		Config:        &config.Config{DataDir: dataDir, SearchConcurrency: concurrency},
		Logger:        zap.NewNop(),
		ExtensionType: TypePlugins,
	})

	for e := range extensions {
		slug := fmt.Sprintf("fixture-%02d", e)
		sourceDir := filepath.Join(dataDir, "plugins", "source", slug)
		indexDir := filepath.Join(dataDir, "plugins", "index", slug)
		if err := os.MkdirAll(sourceDir, 0o750); err != nil {
			t.Fatal(err)
		}
		for f := range files {
			body := fmt.Sprintf("<?php\nfunction needle_%d_%d() {\n\treturn %d;\n}\n", e, f, f)
			if err := os.WriteFile(filepath.Join(sourceDir, fmt.Sprintf("file%02d.php", f)), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		index.IndexDirToFile(sourceDir, filepath.Join(indexDir, "trigrams"), "")

		idx := index.Open(indexDir)
		if idx == nil {
			t.Fatalf("failed to open index for %s", slug)
		}
		t.Cleanup(idx.Close)

		p := &Plugin{IndexedExtension: NewIndexedExtension(), Slug: slug, Name: slug}
		p.SetIndex(idx)
		store.Set(slug, p)
	}
	return store
}

// cancelOnCheck cancels itself the nth time its Err method is called, which
// places the cancellation at a known point of a scan without any timing.
type cancelOnCheck struct {
	context.Context
	cancel context.CancelFunc
	left   atomic.Int64
}

func newCancelOnCheck(n int64) *cancelOnCheck {
	ctx, cancel := context.WithCancel(context.Background())
	c := &cancelOnCheck{Context: ctx, cancel: cancel}
	c.left.Store(n)
	return c
}

func (c *cancelOnCheck) Err() error {
	if c.left.Add(-1) == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

// The search context must reach the per-file loop of an extension scan. With
// one extension, Search itself looks at the context only a few times before
// the scan starts, so a context that expires on its tenth check can only be
// noticed if the scan checks it between files.
func TestSearchStopsScanWhenContextDone(t *testing.T) {
	store := newSearchStore(t, 1, 40, 4)
	opt := &index.SearchOptions{LiteralSearch: true, MaxResults: 1000}

	// Control: the scan completes under a live context.
	results, _, err := store.Search(context.Background(), "needle", opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Matches) != 40 {
		t.Fatalf("control search returned %d results, want 1 extension with 40 matching files", len(results))
	}

	ctx := newCancelOnCheck(10)
	defer ctx.cancel()
	results, _, err = store.Search(ctx, "needle", opt, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %d results and error %v, want context.Canceled: the scan ran to completion without checking its context", len(results), err)
	}
	if results != nil {
		t.Fatalf("got %d results from a cancelled search, want none", len(results))
	}
}
