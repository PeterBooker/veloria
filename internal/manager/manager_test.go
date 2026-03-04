package manager_test

import (
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

	_, err := m.Search("nonexistent", "term", nil)
	if err == nil {
		t.Fatal("expected error for unknown extension type")
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	ds := &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		SearchFunc: func(_ string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, error) {
			return nil, nil
		},
	}

	m := newTestManager(t, ds)
	resp, err := m.Search("plugins", "missing-term", nil)
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
		SearchFunc: func(_ string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, error) {
			return []*repo.SearchResult{
				{Extension: p1, Matches: []*index.FileMatch{{Filename: "a.php", Matches: []*index.Match{{}}}}},
				{Extension: p2, Matches: []*index.FileMatch{{Filename: "b.php", Matches: []*index.Match{{}}}}},
			}, nil
		},
	}

	m := newTestManager(t, ds)
	resp, err := m.Search("plugins", "test", nil)
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
