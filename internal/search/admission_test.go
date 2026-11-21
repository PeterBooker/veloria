package search

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "veloria/internal/api"
	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/repo"
	"veloria/internal/telemetry"
	"veloria/internal/testutil"
)

// acceptingDB is a database/sql driver that accepts the statements a search
// sends while it runs: it answers the row insert with a new id and reports
// one row changed for each update. It stores nothing.
type acceptingDB struct {
	mu      sync.Mutex
	inserts int
}

func (d *acceptingDB) insertCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inserts
}

func (d *acceptingDB) Connect(context.Context) (driver.Conn, error) {
	return &acceptingConn{db: d}, nil
}
func (d *acceptingDB) Driver() driver.Driver            { return d }
func (d *acceptingDB) Open(string) (driver.Conn, error) { return &acceptingConn{db: d}, nil }

type acceptingConn struct{ db *acceptingDB }

func (c *acceptingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported in this test")
}
func (c *acceptingConn) Close() error              { return nil }
func (c *acceptingConn) Begin() (driver.Tx, error) { return acceptingTx{}, nil }

func (c *acceptingConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !strings.HasPrefix(query, `INSERT INTO "searches"`) {
		return nil, errors.New("unexpected query in this test: " + query)
	}
	c.db.mu.Lock()
	c.db.inserts++
	c.db.mu.Unlock()
	return &idRows{id: uuid.NewString()}, nil
}

func (c *acceptingConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(query, `UPDATE "searches"`) {
		return nil, errors.New("unexpected statement in this test: " + query)
	}
	return driver.RowsAffected(1), nil
}

type acceptingTx struct{}

func (acceptingTx) Commit() error   { return nil }
func (acceptingTx) Rollback() error { return nil }

// idRows is the single-row result of INSERT ... RETURNING "id".
type idRows struct {
	id   string
	done bool
}

func (r *idRows) Columns() []string { return []string{"id"} }
func (r *idRows) Close() error      { return nil }
func (r *idRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.id
	return nil
}

// blockingSource is a plugins data source whose searches wait until released.
type blockingSource struct {
	started  chan struct{}
	release  chan struct{}
	searches atomic.Int64
}

func newBlockingSource() *blockingSource {
	return &blockingSource{started: make(chan struct{}, 64), release: make(chan struct{})}
}

func (b *blockingSource) dataSource() repo.DataSource {
	return &testutil.FakeDataSource{
		TypeVal: repo.TypePlugins,
		SearchFunc: func(ctx context.Context, _ string, _ *index.SearchOptions, _ func(int, int)) ([]*repo.SearchResult, int, error) {
			b.searches.Add(1)
			b.started <- struct{}{}
			select {
			case <-b.release:
			case <-ctx.Done():
			}
			return nil, 0, nil
		},
	}
}

// allSlotsFree reports whether every in-flight search slot can be reserved.
func allSlotsFree(m *manager.Manager) bool {
	var held []*manager.SearchSlot
	defer func() {
		for _, slot := range held {
			slot.Release()
		}
	}()
	for range manager.MaxInFlightSearches {
		slot, err := m.ReserveSearch()
		if err != nil {
			return false
		}
		held = append(held, slot)
	}
	return true
}

// When MaxInFlightSearches searches are running, the next one is refused with
// 503 and Retry-After before a row is stored or a goroutine started, on both
// the web form and the API, and the slots come back when the searches end.
func TestSearchAdmission_RefusesOverCap(t *testing.T) {
	require.NoError(t, telemetry.InitMetrics())

	type result struct {
		code       int
		retryAfter string
		body       string
	}
	routes := map[string]func(f *searchFixture) result{
		"web form": func(f *searchFixture) result {
			w := f.postForm(url.Values{"term": {"wp_remote_get"}, "repo": {"plugins"}})
			return result{w.Code, w.Header().Get("Retry-After"), w.Body.String()}
		},
		"API": func(f *searchFixture) result {
			w := f.postJSON(`{"term":"wp_remote_get","repo":"plugins"}`)
			return result{w.Code, w.Header().Get("Retry-After"), w.Body.String()}
		},
	}

	for name, post := range routes {
		t.Run(name, func(t *testing.T) {
			db := &acceptingDB{}
			source := newBlockingSource()
			f := newSearchFixtureWith(t, sql.OpenDB(db), source.dataSource())
			released := false
			defer func() {
				if !released {
					close(source.release)
				}
			}()

			// Fill the cap: each search is accepted and is now running.
			for i := range manager.MaxInFlightSearches {
				got := post(f)
				require.Less(t, got.code, 300, "search %d of %d should be accepted, got %d: %s", i+1, manager.MaxInFlightSearches, got.code, got.body)
				select {
				case <-source.started:
				case <-time.After(5 * time.Second):
					t.Fatalf("search %d never started", i+1)
				}
			}
			require.Equal(t, manager.MaxInFlightSearches, db.insertCount())

			// One more is refused, and leaves nothing behind.
			got := post(f)
			assert.Equal(t, http.StatusServiceUnavailable, got.code)
			assert.Equal(t, searchBusyRetryAfter, got.retryAfter, "Retry-After header")
			assert.Contains(t, strings.ToLower(got.body), "search", "body should say why: %s", got.body)
			assert.Equal(t, manager.MaxInFlightSearches, db.insertCount(), "a refused search must not store a row")

			// So is a direct manager search, which is how MCP searches start.
			// The timeout only matters if it is wrongly admitted.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, err := f.manager.Search(ctx, "plugins", "wp_remote_get", nil)
			cancel()
			assert.ErrorIs(t, err, manager.ErrSearchBusy)

			assert.EqualValues(t, manager.MaxInFlightSearches, source.searches.Load(), "a refused search must not reach the search engine")

			// When the running searches finish, their slots are returned.
			close(source.release)
			released = true
			require.Eventually(t, func() bool { return allSlotsFree(f.manager) }, 5*time.Second, 5*time.Millisecond,
				"search slots were not returned after the searches finished")

			got = post(f)
			assert.Less(t, got.code, 300, "a search after the others finished should be accepted, got %d", got.code)
			require.Eventually(t, func() bool { return allSlotsFree(f.manager) }, 5*time.Second, 5*time.Millisecond)
		})
	}
}

// The API refusal is a JSON error like every other API error.
func TestCreateSearchV1_BusyResponseIsJSON(t *testing.T) {
	f := newSearchFixture(t)
	for range manager.MaxInFlightSearches {
		slot, err := f.manager.ReserveSearch()
		require.NoError(t, err)
		defer slot.Release()
	}

	w := f.postJSON(`{"term":"wp_remote_get","repo":"plugins"}`)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, searchBusyRetryAfter, w.Header().Get("Retry-After"))
	var body api.APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, manager.ErrSearchBusy.Error(), body.Message)
	assert.Empty(t, f.pool.recorded(), "a refused search must not touch the database")
}

// A request that reserves a slot and then fails to store its row must give
// the slot back, or a database outage would use up every slot for good.
func TestSearchAdmission_ReleasesSlotWhenInsertFails(t *testing.T) {
	f := newSearchFixture(t) // every statement fails

	for range 3 * manager.MaxInFlightSearches {
		w := f.postForm(url.Values{"term": {"wp_remote_get"}, "repo": {"plugins"}})
		require.Contains(t, w.Body.String(), "Failed to create search")

		rec := f.postJSON(`{"term":"wp_remote_get","repo":"plugins"}`)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
	}

	assert.True(t, allSlotsFree(f.manager), "failed inserts leaked search slots")
}

// An invalid request is rejected before it takes a slot.
func TestSearchAdmission_InvalidRequestTakesNoSlot(t *testing.T) {
	f := newSearchFixture(t)
	for range manager.MaxInFlightSearches {
		slot, err := f.manager.ReserveSearch()
		require.NoError(t, err)
		defer slot.Release()
	}

	// With every slot taken, validation errors still win over "busy".
	w := f.postJSON(`{"term":"x","repo":"elsewhere"}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(url.Values{"term": {"x"}, "repo": {"elsewhere"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	SubmitSearch(f.deps).ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Data source must be one of")
}
