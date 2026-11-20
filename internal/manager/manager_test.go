package manager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"veloria/internal/api"
	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/repo"
	"veloria/internal/testutil"
)

// newTestManager creates a Manager with fake DataSources for testing.
// It bypasses the normal startup flow (Load, startUpdater) to give
// deterministic behaviour in unit tests.
func newTestManager(t *testing.T, sources ...repo.DataSource) *manager.Manager {
	t.Helper()

	// Use a no-op logger.
	l := testutil.NopLogger()

	m, err := manager.NewTestManager(l, sources)
	if err != nil {
		t.Fatalf("newTestManager: %v", err)
	}
	return m
}

func TestSearch_UnknownType(t *testing.T) {
	m := newTestManager(t)

	_, err := m.Search(context.Background(), "nonexistent", "term", nil)
	if err == nil {
		t.Fatal("expected error for unknown extension type")
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		SearchFunc: func(_ context.Context, _ string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, int, error) {
			return nil, 0, nil
		},
	}

	m := newTestManager(t, ds)
	resp, err := m.Search(context.Background(), "plugins", "missing-term", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected 0 results, got %d", resp.Total)
	}
}

func TestSearch_SortsByActiveInstalls(t *testing.T) {
	p1 := testutil.SamplePlugin()
	p1.Slug = "low-installs"
	p1.ActiveInstalls = 100

	p2 := testutil.SamplePlugin()
	p2.Slug = "high-installs"
	p2.ActiveInstalls = 10000

	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		SearchFunc: func(_ context.Context, _ string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, int, error) {
			return []*repo.SearchResult{
				{Extension: p1, Matches: []*index.FileMatch{{Filename: "a.php", Matches: []*index.Match{{}}}}},
				{Extension: p2, Matches: []*index.FileMatch{{Filename: "b.php", Matches: []*index.Match{{}}}}},
			}, 2, nil
		},
	}

	m := newTestManager(t, ds)
	resp, err := m.Search(context.Background(), "plugins", "test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected 2 results, got %d", resp.Total)
	}
	if resp.Results[0].Slug != "high-installs" {
		t.Errorf("expected first result to be high-installs, got %s", resp.Results[0].Slug)
	}
	if resp.Results[1].Slug != "low-installs" {
		t.Errorf("expected second result to be low-installs, got %s", resp.Results[1].Slug)
	}
}

func TestSubmitReindex_UnknownType(t *testing.T) {
	m := newTestManager(t)
	if err := m.SubmitReindex("nonexistent", "slug"); err == nil {
		t.Error("expected SubmitReindex to return error for unknown type")
	}
}

func TestSubmitReindex_NotFound(t *testing.T) {
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		MakeReindexTaskFunc: func(slug string) (repo.IndexTask, bool) {
			return repo.IndexTask{}, false
		},
	}

	m := newTestManager(t, ds)
	if err := m.SubmitReindex("plugins", "not-found"); err == nil {
		t.Error("expected SubmitReindex to return error for missing slug")
	}
}

func TestSubmitReindex_Success(t *testing.T) {
	ran := false
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		MakeReindexTaskFunc: func(slug string) (repo.IndexTask, bool) {
			return repo.IndexTask{
				ExtensionType: repo.TypePlugins,
				Slug:          slug,
				Run:           func() error { ran = true; return nil },
			}, true
		},
	}

	m := newTestManager(t, ds)
	if err := m.SubmitReindex("plugins", "test-plugin"); err != nil {
		t.Errorf("expected SubmitReindex to succeed, got: %v", err)
	}
	// The task is queued, not immediately run.
	_ = ran
}

func TestGetExtension_Found(t *testing.T) {
	p := testutil.SamplePlugin()
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		GetExtensionFunc: func(slug string) (repo.Extension, bool) {
			if slug == "test-plugin" {
				return p, true
			}
			return nil, false
		},
	}

	m := newTestManager(t, ds)
	ext, ok := m.GetExtension("plugins", "test-plugin")
	if !ok {
		t.Fatal("expected to find extension")
	}
	if ext.GetSlug() != "test-plugin" {
		t.Errorf("expected slug test-plugin, got %s", ext.GetSlug())
	}
}

func TestGetExtension_NotFound(t *testing.T) {
	ds := &testutil.FakeDataSource{TypeVal: repo.TypePlugins}

	m := newTestManager(t, ds)
	_, ok := m.GetExtension("plugins", "nonexistent")
	if ok {
		t.Error("expected extension not to be found")
	}
}

func TestGetExtension_UnknownType(t *testing.T) {
	m := newTestManager(t)
	_, ok := m.GetExtension("nonexistent", "slug")
	if ok {
		t.Error("expected extension not to be found for unknown type")
	}
}

func TestStats(t *testing.T) {
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		StatsFunc: func() (int, int) {
			return 100, 50
		},
	}

	m := newTestManager(t, ds)

	total, indexed, ok := m.Stats("plugins")
	if !ok {
		t.Fatal("expected ok=true for known type")
	}
	if total != 100 || indexed != 50 {
		t.Errorf("expected (100, 50), got (%d, %d)", total, indexed)
	}

	_, _, ok = m.Stats("nonexistent")
	if ok {
		t.Error("expected ok=false for unknown type")
	}
}

func TestIndexStatus(t *testing.T) {
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypeThemes,
		IndexStatusFunc: func() map[string]bool {
			return map[string]bool{"twentytwentyfive": true, "astra": false}
		},
	}

	m := newTestManager(t, ds)
	status := m.IndexStatus("themes")
	if !status["twentytwentyfive"] {
		t.Error("expected twentytwentyfive to be indexed")
	}
	if status["astra"] {
		t.Error("expected astra to not be indexed")
	}

	if m.IndexStatus("nonexistent") != nil {
		t.Error("expected nil for unknown type")
	}
}

func TestResolveSourceDir_UnknownType(t *testing.T) {
	m := newTestManager(t)
	_, err := m.ResolveSourceDir("nonexistent", "slug")
	if err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestErrExtNotFound_ImplementsStatusCoder(t *testing.T) {
	var sc api.StatusCoder
	assert.ErrorAs(t, manager.ErrExtNotFound, &sc)
	assert.Equal(t, 404, sc.StatusCode())
}

func TestErrQueueFull_ImplementsStatusCoder(t *testing.T) {
	var sc api.StatusCoder
	assert.ErrorAs(t, manager.ErrQueueFull, &sc)
	assert.Equal(t, 429, sc.StatusCode())
}

func TestSentinelErrors_WorkWithErrorsIs(t *testing.T) {
	assert.True(t, errors.Is(manager.ErrExtNotFound, manager.ErrExtNotFound))
	assert.True(t, errors.Is(manager.ErrQueueFull, manager.ErrQueueFull))
}

// At most MaxInFlightSearches searches hold a slot; the next is refused at
// once instead of queued.
func TestReserveSearch_RefusesOverCap(t *testing.T) {
	m := newTestManager(t)

	slots := make([]*manager.SearchSlot, 0, manager.MaxInFlightSearches)
	for i := range manager.MaxInFlightSearches {
		slot, err := m.ReserveSearch()
		if err != nil {
			t.Fatalf("reservation %d of %d refused: %v", i+1, manager.MaxInFlightSearches, err)
		}
		slots = append(slots, slot)
	}

	_, err := m.ReserveSearch()
	if !errors.Is(err, manager.ErrSearchBusy) {
		t.Fatalf("reservation over the cap: got %v, want ErrSearchBusy", err)
	}
	var sc api.StatusCoder
	if !errors.As(err, &sc) || sc.StatusCode() != 503 {
		t.Fatalf("ErrSearchBusy should map to HTTP 503, got %v", err)
	}

	// Releasing twice frees one slot, not two.
	slots[0].Release()
	slots[0].Release()
	if _, err := m.ReserveSearch(); err != nil {
		t.Fatalf("reservation after a release refused: %v", err)
	}
	if _, err := m.ReserveSearch(); !errors.Is(err, manager.ErrSearchBusy) {
		t.Fatalf("double release freed a second slot: got %v, want ErrSearchBusy", err)
	}
}

// Manager.Search is the entry point MCP uses: it takes a slot for the call,
// returns ErrSearchBusy without searching when none is free, and gives the
// slot back when it returns.
func TestSearch_RefusedWhenBusy(t *testing.T) {
	searches := 0
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		SearchFunc: func(_ context.Context, _ string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, int, error) {
			searches++
			return nil, 0, nil
		},
	}
	m := newTestManager(t, ds)

	// Slots are returned: many more sequential searches than slots succeed.
	for range 3 * manager.MaxInFlightSearches {
		if _, err := m.Search(context.Background(), "plugins", "term", nil); err != nil {
			t.Fatalf("sequential search refused: %v", err)
		}
	}
	// An error path returns the slot too.
	for range 3 * manager.MaxInFlightSearches {
		if _, err := m.Search(context.Background(), "nonexistent", "term", nil); errors.Is(err, manager.ErrSearchBusy) {
			t.Fatal("failed searches leaked their slots")
		}
	}

	var held []*manager.SearchSlot
	for range manager.MaxInFlightSearches {
		slot, err := m.ReserveSearch()
		if err != nil {
			t.Fatalf("unexpected refusal: %v", err)
		}
		held = append(held, slot)
	}

	before := searches
	if _, err := m.Search(context.Background(), "plugins", "term", nil); !errors.Is(err, manager.ErrSearchBusy) {
		t.Fatalf("search with every slot taken: got %v, want ErrSearchBusy", err)
	}
	if searches != before {
		t.Fatal("a refused search still reached the data source")
	}

	// A reserved slot can run its search while the cap is reached.
	if _, err := held[0].Search(context.Background(), "plugins", "term", nil); err != nil {
		t.Fatalf("search through a held slot failed: %v", err)
	}
	for _, slot := range held {
		slot.Release()
	}
	if _, err := m.Search(context.Background(), "plugins", "term", nil); err != nil {
		t.Fatalf("search after slots were released refused: %v", err)
	}
}
