# Testing

## Running Tests

```bash
# All unit tests
go test ./...

# Specific package
go test ./internal/manager/

# Verbose output
go test -v ./internal/manager/

# Integration tests (requires Docker services)
go test -tags integration ./...
```

## Test Infrastructure

### Fixtures (`internal/testutil/fixtures.go`)

Pre-built sample data for tests:

```go
testutil.SamplePlugin()  // *repo.Plugin with realistic defaults
testutil.SampleTheme()   // *repo.Theme
testutil.SampleCore()    // *repo.Core
testutil.SampleConfig()  // *config.Config with test-friendly values
testutil.NopLogger()     // *zerolog.Logger that discards output
```

### Hand-Written Fakes (`internal/testutil/mocks.go`)

Veloria uses hand-written fakes with function fields instead of code-generated mocks. Each method delegates to its function field when set, falling back to a safe zero-value default.

#### FakeDataSource

Implements `repo.DataSource` for testing the Manager without real stores:

```go
ds := &testutil.FakeDataSource{
    TypeVal: repo.TypePlugins,
    SearchFunc: func(term string, opt *index.SearchOptions, fn func(int, int)) ([]*repo.SearchResult, error) {
        return []*repo.SearchResult{
            {Extension: testutil.SamplePlugin(), Matches: []*index.FileMatch{{Filename: "main.php", Matches: []*index.Match{{}}}}},
        }, nil
    },
}

m := newTestManager(t, ds)
resp, err := m.Search("plugins", "test", nil)
```

Available function fields:

| Field | Signature |
|---|---|
| `LoadFunc` | `func() error` |
| `StatsFunc` | `func() (int, int)` |
| `IndexStatusFunc` | `func() map[string]bool` |
| `SearchFunc` | `func(term string, opt *index.SearchOptions, fn func(int, int)) ([]*repo.SearchResult, error)` |
| `PrepareUpdatesFunc` | `func() []repo.IndexTask` |
| `ResumeUnindexedFunc` | `func() []repo.IndexTask` |
| `GetExtensionFunc` | `func(slug string) (repo.Extension, bool)` |
| `MakeReindexTaskFunc` | `func(slug string) (repo.IndexTask, bool)` |
| `ResolveSourceDirFunc` | `func(slug string) (string, error)` |

#### Web Interface Fakes

For testing web handlers without a real Manager:

```go
// FakeSearchService implements web.SearchService
svc := &testutil.FakeSearchService{
    SearchFunc: func(repoType, term string, params *manager.SearchParams) (*manager.SearchResponse, error) {
        return &manager.SearchResponse{Total: 0}, nil
    },
}

// FakeReindexService implements web.ReindexService
reindex := &testutil.FakeReindexService{
    SubmitReindexFunc: func(repoType, slug string) bool { return true },
}

// FakeSourceResolver implements web.SourceResolver
resolver := &testutil.FakeSourceResolver{
    ResolveSourceDirFunc: func(repoType, slug string) (string, error) {
        return "/tmp/source/test-plugin", nil
    },
}

// FakeStatsProvider implements web.StatsProvider
stats := &testutil.FakeStatsProvider{
    StatsFunc: func(repoType string) (int, int, bool) { return 100, 50, true },
    IndexStatusFunc: func(repoType string) map[string]bool {
        return map[string]bool{"test-plugin": true}
    },
}
```

### Test Manager

`manager.NewTestManager()` creates a Manager without loading data or starting the updater goroutine:

```go
func newTestManager(t *testing.T, sources ...repo.DataSource) *manager.Manager {
    t.Helper()
    l := testutil.NopLogger()
    m, err := manager.NewTestManager(l, sources)
    if err != nil {
        t.Fatalf("newTestManager: %v", err)
    }
    return m
}
```

This is used in `internal/manager/manager_test.go` for fast, deterministic unit tests.

## Writing Tests

### Testing Manager Methods

```go
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
    if resp.Results[0].Slug != "high-installs" {
        t.Errorf("expected first result to be high-installs, got %s", resp.Results[0].Slug)
    }
}
```

### Testing API Handlers

API handlers accept their dependencies as parameters, making them straightforward to test with `httptest`:

```go
func TestViewPluginV1_NotFound(t *testing.T) {
    db := setupTestDB(t) // your DB setup helper

    req := httptest.NewRequest("GET", "/api/v1/plugin/"+uuid.New().String(), nil)
    rr := httptest.NewRecorder()

    handler := plugin.ViewPluginV1(db)
    handler.ServeHTTP(rr, req)

    if rr.Code != http.StatusOK { // API returns 200 with error payload
        t.Errorf("expected 200, got %d", rr.Code)
    }
    // Check JSON body for error response
}
```

### Testing Web Handlers

Web handlers take `*web.Deps`, so you can inject fakes for any capability:

```go
deps := &web.Deps{
    Search:  &testutil.FakeSearchService{...},
    Reindex: &testutil.FakeReindexService{...},
    Stats:   &testutil.FakeStatsProvider{...},
    Sources: &testutil.FakeSourceResolver{...},
    // ...
}
```

### Integration Tests

Integration tests use the `//go:build integration` build tag and `testcontainers-go` to spin up real PostgreSQL instances:

```go
//go:build integration

func TestPluginStore_Integration(t *testing.T) {
    // Uses testcontainers-go to start PostgreSQL
    // Tests real DB operations
}
```

Run with:
```bash
go test -tags integration ./...
```

## Compile-Time Interface Checks

Each fake includes a compile-time assertion to catch interface drift:

```go
var _ repo.DataSource = (*FakeDataSource)(nil)
```

If the interface changes, the fake will fail to compile, alerting you to update it.
