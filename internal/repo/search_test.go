package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestValidExtensionType(t *testing.T) {
	for _, name := range []string{"plugins", "themes", "cores"} {
		if !ValidExtensionType(name) {
			t.Errorf("ValidExtensionType(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "plugin", "Plugins", "plugins ", "searches", "Buy cheap things at spam-site.example"} {
		if ValidExtensionType(name) {
			t.Errorf("ValidExtensionType(%q) = true, want false", name)
		}
	}
}

// scanGauge counts the extension scans running at one moment and remembers
// the highest count seen.
type scanGauge struct {
	running atomic.Int64
	peak    atomic.Int64
}

// watch makes store report its scans to the gauge. Each scan is held for a
// moment so that scans from concurrent searches overlap.
func (g *scanGauge) watch(store *ExtensionStore[*Plugin]) {
	scan := store.scanExtension
	store.scanExtension = func(ctx context.Context, ext *Plugin, cs *index.CompiledSearch) (*index.SearchResponse, error) {
		n := g.running.Add(1)
		defer g.running.Add(-1)
		for {
			peak := g.peak.Load()
			if n <= peak || g.peak.CompareAndSwap(peak, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		return scan(ctx, ext, cs)
	}
}

// SEARCH_CONCURRENCY is a limit for the process: however many searches run at
// once, the extension scans in progress never exceed it.
func TestSearchSharesScanLimitAcrossCalls(t *testing.T) {
	const (
		concurrency = 3
		callers     = 4
		extensions  = 8
	)
	store := newSearchStore(t, extensions, 1, concurrency)
	var gauge scanGauge
	gauge.watch(store)

	opt := &index.SearchOptions{LiteralSearch: true, MaxResults: 1000}
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, _, err := store.Search(context.Background(), "needle", opt, nil)
			if err == nil && len(results) != extensions {
				err = fmt.Errorf("got %d results, want %d", len(results), extensions)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	if peak := gauge.peak.Load(); peak > concurrency {
		t.Fatalf("%d concurrent searches ran %d extension scans at once, want at most SEARCH_CONCURRENCY=%d", callers, peak, concurrency)
	}
	if n := len(store.searchLimiter.slots); n != 0 {
		t.Fatalf("%d scan slots still held after every search returned", n)
	}
}

// The limiter is shared between stores, so searches of different data sources
// count against the same limit.
func TestSearchSharesScanLimitAcrossStores(t *testing.T) {
	const (
		concurrency = 2
		extensions  = 6
	)
	limiter := NewSearchLimiter(concurrency)
	var gauge scanGauge
	stores := make([]*ExtensionStore[*Plugin], 3)
	for i := range stores {
		stores[i] = newSearchStore(t, extensions, 1, 24)
		stores[i].searchLimiter = limiter
		gauge.watch(stores[i])
	}

	opt := &index.SearchOptions{LiteralSearch: true, MaxResults: 1000}
	var wg sync.WaitGroup
	for _, store := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := store.Search(context.Background(), "needle", opt, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if peak := gauge.peak.Load(); peak > concurrency {
		t.Fatalf("searches on %d stores ran %d extension scans at once, want at most %d", len(stores), peak, concurrency)
	}
}

func TestNewExtensionStoreUsesSharedLimiter(t *testing.T) {
	limiter := NewSearchLimiter(5)
	cfg := StoreConfig[*Plugin]{
		Ctx:           context.Background(),
		Config:        &config.Config{DataDir: t.TempDir(), SearchConcurrency: 24},
		Logger:        zap.NewNop(),
		ExtensionType: TypePlugins,
		SearchLimiter: limiter,
	}
	if got := NewExtensionStore(cfg).searchLimiter; got != limiter {
		t.Fatal("store did not keep the limiter it was given")
	}

	cfg.SearchLimiter = nil
	if got := cap(NewExtensionStore(cfg).searchLimiter.slots); got != 24 {
		t.Fatalf("store without a shared limiter allows %d scans, want SEARCH_CONCURRENCY=24", got)
	}
}

// A search whose context ends while it is taking a scan slot must give the
// slot back. The limiter is shared by the whole process, so a slot lost on
// this path would be lost to every later search.
func TestSearchReturnsScanSlotsWhenCancelled(t *testing.T) {
	const concurrency = 2
	store := newSearchStore(t, 4, 1, concurrency)
	opt := &index.SearchOptions{LiteralSearch: true, MaxResults: 1000}

	// Cancel on each of the first checks in turn, to cover every point of
	// the dispatch loop, including just after a slot was taken.
	for n := int64(1); n <= 12; n++ {
		ctx := newCancelOnCheck(n)
		_, _, _ = store.Search(ctx, "needle", opt, nil)
		ctx.cancel()

		if held := len(store.searchLimiter.slots); held != 0 {
			t.Fatalf("context cancelled on check %d: %d scan slots still held after Search returned", n, held)
		}
	}

	results, _, err := store.Search(context.Background(), "needle", opt, nil)
	if err != nil || len(results) != 4 {
		t.Fatalf("search after cancelled searches: got %d results and error %v, want 4 results", len(results), err)
	}
}
