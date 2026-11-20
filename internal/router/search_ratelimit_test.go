package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"veloria/internal/config"
	"veloria/internal/service"
	"veloria/internal/web"
)

// newSearchTestRouter builds the real router with no services connected, so the
// search handlers answer 503 themselves and a 429 can only come from a rate
// limiter in front of them.
func newSearchTestRouter(rateLimit bool) http.Handler {
	reg := &service.Registry{}
	return New(RouterDeps{
		Logger:   zap.NewNop(),
		Registry: reg,
		WebDeps:  web.NewDeps(reg, nil, &config.Config{}),
		Options:  Options{HandlerTimeout: 30 * time.Second, RateLimitEnabled: rateLimit},
	})
}

func postSearchForm(h http.Handler, remoteAddr string) int {
	r := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader("term=x&repo=plugins"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// The web search form is limited to ten submissions a minute per address,
// like POST /api/v1/search.
func TestWebSearchIsRateLimited(t *testing.T) {
	const peer = "203.0.113.9:40000"
	h := newSearchTestRouter(true)

	for i := 1; i <= 10; i++ {
		if code := postSearchForm(h, peer); code == http.StatusTooManyRequests {
			t.Fatalf("request %d from one address was rate limited, want the first ten allowed", i)
		}
	}
	if code := postSearchForm(h, peer); code != http.StatusTooManyRequests {
		t.Fatalf("eleventh request in a minute from one address: got HTTP %d, want 429", code)
	}

	// The limit is per address.
	if code := postSearchForm(h, "198.51.100.7:40000"); code == http.StatusTooManyRequests {
		t.Fatal("a second address was rate limited by the first address's requests")
	}
}

func TestWebSearchRateLimitCanBeDisabled(t *testing.T) {
	h := newSearchTestRouter(false)
	for i := 1; i <= 15; i++ {
		if code := postSearchForm(h, "203.0.113.9:40000"); code == http.StatusTooManyRequests {
			t.Fatalf("request %d was rate limited with rate limiting disabled", i)
		}
	}
}
