package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	api "veloria/internal/api"
	"veloria/internal/config"
	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/repo"
	"veloria/internal/service"
	"veloria/internal/testutil"
	"veloria/internal/web"
)

// errNoDatabase is what every statement sent to a recordingPool fails with.
var errNoDatabase = errors.New("no database in this test")

// recordingPool is a gorm.ConnPool that records each statement it is asked to
// run and then fails it. It lets a handler test tell "rejected before touching
// the database" from "tried to store a search" without a real Postgres.
type recordingPool struct {
	mu         sync.Mutex
	statements []string
}

func (p *recordingPool) record(query string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statements = append(p.statements, query)
}

func (p *recordingPool) recorded() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.statements...)
}

// inserted reports whether a search row insert was attempted.
func (p *recordingPool) inserted() bool {
	for _, q := range p.recorded() {
		if strings.HasPrefix(q, `INSERT INTO "searches"`) {
			return true
		}
	}
	return false
}

func (p *recordingPool) PrepareContext(_ context.Context, query string) (*sql.Stmt, error) {
	p.record(query)
	return nil, errNoDatabase
}

func (p *recordingPool) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	p.record(query)
	return nil, errNoDatabase
}

func (p *recordingPool) QueryContext(_ context.Context, query string, _ ...any) (*sql.Rows, error) {
	p.record(query)
	return nil, errNoDatabase
}

func (p *recordingPool) QueryRowContext(_ context.Context, query string, _ ...any) *sql.Row {
	p.record(query)
	return &sql.Row{}
}

// nopStorage is a result store that accepts and returns nothing.
type nopStorage struct{}

func (nopStorage) UploadResult(context.Context, string, proto.Message) (int64, error) { return 0, nil }
func (nopStorage) DownloadResult(context.Context, string, proto.Message) error        { return nil }
func (nopStorage) DeleteResult(context.Context, string) error                         { return nil }
func (nopStorage) EnsureBucket(context.Context) error                                 { return nil }
func (nopStorage) GetObjectKey(searchID string) string                                { return searchID }

// searchFixture is a registry with search enabled: a database that records
// and fails every statement, a no-op result store and a manager with one
// empty plugins source.
type searchFixture struct {
	reg     *service.Registry
	deps    *web.Deps
	pool    *recordingPool
	manager *manager.Manager
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()

	pool := &recordingPool{}
	f := newSearchFixtureWith(t, pool, &testutil.FakeDataSource{TypeVal: repo.TypePlugins})
	f.pool = pool
	return f
}

// newSearchFixtureWith builds the fixture on a given database connection and
// plugins data source.
func newSearchFixtureWith(t *testing.T, conn gorm.ConnPool, plugins repo.DataSource) *searchFixture {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	m, err := manager.NewTestManager(testutil.NopLogger(), []repo.DataSource{plugins})
	require.NoError(t, err)

	reg := &service.Registry{}
	reg.SetDB(db, nil)
	reg.SetS3(nopStorage{})
	reg.SetManager(m)

	return &searchFixture{
		reg:     reg,
		deps:    web.NewDeps(reg, nil, &config.Config{}),
		manager: m,
	}
}

// postForm sends a search form to the web handler.
func (f *searchFixture) postForm(form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	SubmitSearch(f.deps).ServeHTTP(w, r)
	return w
}

// postJSON sends a search request to the API handler.
func (f *searchFixture) postJSON(body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/search", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	CreateSearchV1(f.reg).ServeHTTP(w, r)
	return w
}

func jsonBody(t *testing.T, req SearchRequest) string {
	t.Helper()
	b, err := json.Marshal(req)
	require.NoError(t, err)
	return string(b)
}

// Control for the tests below: a valid request does reach the insert, so
// "no insert" in the rejection tests means the handler stopped first.
func TestSearchFixtureSeesInserts(t *testing.T) {
	f := newSearchFixture(t)
	w := f.postForm(url.Values{"term": {"wp_remote_get"}, "repo": {"plugins"}})
	assert.Contains(t, w.Body.String(), "Failed to create search")
	assert.True(t, f.pool.inserted(), "valid web search should attempt an insert, got %q", f.pool.recorded())

	f = newSearchFixture(t)
	w = f.postJSON(`{"term":"wp_remote_get","repo":"plugins","file_match":"\\.php$"}`)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.True(t, f.pool.inserted(), "valid API search should attempt an insert, got %q", f.pool.recorded())
}

// The web form must refuse a data source outside plugins, themes and cores
// before it stores a row: the value is shown on public search listings, which
// only moderate the term.
func TestSubmitSearch_RejectsUnknownRepo(t *testing.T) {
	f := newSearchFixture(t)

	w := f.postForm(url.Values{
		"term":       {"wp_remote_get"},
		"repo":       {"Buy cheap things at spam-site.example"},
		"visibility": {"public"},
	})

	assert.Contains(t, w.Body.String(), "Data source must be one of: plugins, themes, cores")
	assert.Empty(t, f.pool.recorded(), "an unknown repo must be rejected before any database statement")
}

func TestSubmitSearch_RejectsOversizedInput(t *testing.T) {
	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{
			name: "term over 255 bytes",
			form: url.Values{"term": {strings.Repeat("a", index.MaxPatternLen+1)}, "repo": {"plugins"}},
			want: "Search term must be at most 255 bytes",
		},
		{
			name: "file type over the filter limit",
			form: url.Values{"term": {"wp_remote_get"}, "repo": {"plugins"}, "filetype": {strings.Repeat("a", index.MaxFileFilterLen)}},
			want: "Invalid file type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSearchFixture(t)
			w := f.postForm(tt.form)
			assert.Contains(t, w.Body.String(), tt.want)
			assert.Empty(t, f.pool.recorded(), "oversized input must be rejected before any database statement")
		})
	}
}

func TestSubmitSearch_AcceptsTermAtLimit(t *testing.T) {
	f := newSearchFixture(t)
	f.postForm(url.Values{"term": {strings.Repeat("a", index.MaxPatternLen)}, "repo": {"themes"}})
	assert.True(t, f.pool.inserted(), "a 255-byte term on a valid repo should be accepted")
}

// The API must validate term, repo and both file filters when the request is
// accepted, answering 400 before it stores a row or starts a search.
func TestCreateSearchV1_RejectsInvalidInput(t *testing.T) {
	long := strings.Repeat("a", 256)
	// Four kilobytes of counted repetition: accepted by regexp.Compile, and
	// costly to run against every candidate file path.
	heavy := strings.Repeat("(?:.?){1000}", 300) + `\.zzz$`

	tests := []struct {
		name string
		req  SearchRequest
		want string
	}{
		{"term over 255 bytes", SearchRequest{Term: long, Repo: "plugins"}, "term must be at most 255 bytes"},
		{"unknown repo", SearchRequest{Term: "x", Repo: "elsewhere"}, "repo must be one of: plugins, themes, cores"},
		{"file_match over 255 bytes", SearchRequest{Term: "x", Repo: "plugins", FileMatch: long}, "invalid file_match: file filter is longer than 255 bytes"},
		{"exclude_file_match over 255 bytes", SearchRequest{Term: "x", Repo: "plugins", ExcludeFileMatch: long}, "invalid exclude_file_match: file filter is longer than 255 bytes"},
		{"heavy file_match", SearchRequest{Term: "x", Repo: "plugins", FileMatch: heavy}, "invalid file_match: file filter is longer than 255 bytes"},
		{"file_match that does not compile", SearchRequest{Term: "x", Repo: "plugins", FileMatch: `\.(php`}, "invalid file_match: "},
		{"exclude_file_match that does not compile", SearchRequest{Term: "x", Repo: "plugins", ExcludeFileMatch: `[`}, "invalid exclude_file_match: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSearchFixture(t)

			w := f.postJSON(jsonBody(t, tt.req))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			var body api.APIError
			require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
			assert.Contains(t, body.Message, tt.want)
			assert.Empty(t, f.pool.recorded(), "invalid input must be rejected before any database statement")
		})
	}
}

// An API body over 1 MiB is refused with 413 instead of being buffered.
func TestCreateSearchV1_RejectsOversizedBody(t *testing.T) {
	f := newSearchFixture(t)

	w := f.postJSON(`{"term":"` + strings.Repeat("a", 2*api.MaxJSONBodyBytes) + `"}`)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Empty(t, f.pool.recorded())
}
