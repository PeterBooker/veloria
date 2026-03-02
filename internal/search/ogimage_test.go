package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"veloria/assets"
	"veloria/internal/config"
	ogimage "veloria/internal/image"
	"veloria/internal/service"
	"veloria/internal/web"
)

func newTestOGGen(t *testing.T) *ogimage.Generator {
	t.Helper()
	gen, err := ogimage.New(assets.FS)
	require.NoError(t, err)
	return gen
}

func newMinimalDeps(t *testing.T) *web.Deps {
	t.Helper()
	c := &config.Config{AppURL: "https://test.example.com"}
	return &web.Deps{Registry: &service.Registry{}, Config: c}
}

// fakeCache is a synchronous in-memory cache for testing.
type fakeCache struct {
	mu   sync.RWMutex
	data map[string]any
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string]any)}
}

func (c *fakeCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[key]
	return v, ok
}

func (c *fakeCache) Set(key string, value any, _ time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value
}

func (c *fakeCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

func (c *fakeCache) Close() {}

func TestOGImage_NilDB(t *testing.T) {
	d := newMinimalDeps(t)
	gen := newTestOGGen(t)

	handler := OGImage(d, gen)
	r := httptest.NewRequest(http.MethodGet, "/search/550e8400-e29b-41d4-a716-446655440000/og.png", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("uuid", "550e8400-e29b-41d4-a716-446655440000")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestOGImage_CacheHit(t *testing.T) {
	fc := newFakeCache()

	d := newMinimalDeps(t)
	d.Cache = fc

	gen := newTestOGGen(t)

	testID := "550e8400-e29b-41d4-a716-446655440000"
	fakeImage := []byte("PNG-fake-data")
	fc.Set("og:"+testID, fakeImage, 0)

	handler := OGImage(d, gen)

	r := httptest.NewRequest(http.MethodGet, "/search/"+testID+"/og.png", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("uuid", testID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	handler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Cache-Control"), "immutable")
	assert.Equal(t, fakeImage, w.Body.Bytes())
}
