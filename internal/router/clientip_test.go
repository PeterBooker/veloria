package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"veloria/internal/service"
)

// searchPath is under the 10 requests/minute per-IP limiter. With an empty
// registry the handler answers 503 once a request passes the limiter.
const (
	searchPath  = "/api/v1/search/00000000-0000-4000-8000-000000000001"
	searchLimit = 10
)

func newClientIPTestRouter(t *testing.T, logger *zap.Logger, opts Options) http.Handler {
	t.Helper()
	opts.HandlerTimeout = 5 * time.Second
	opts.RateLimitEnabled = true
	return New(RouterDeps{
		Logger:   logger,
		Registry: &service.Registry{},
		Options:  opts,
	})
}

// get sends one request for searchPath whose TCP peer address is peer.
func get(t *testing.T, h http.Handler, peer string, headers map[string]string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, searchPath, nil)
	req.RemoteAddr = peer
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRateLimit_ForwardingHeadersDoNotChangeKey(t *testing.T) {
	t.Parallel()
	h := newClientIPTestRouter(t, zap.NewNop(), Options{})

	for i := 1; i <= searchLimit+1; i++ {
		addr := fmt.Sprintf("192.0.2.%d", i)
		code := get(t, h, "203.0.113.10:40000", map[string]string{
			"X-Forwarded-For": addr,
			"X-Real-IP":       addr,
			"True-Client-IP":  addr,
		})
		if limited := code == http.StatusTooManyRequests; limited != (i > searchLimit) {
			t.Fatalf("request %d: status %d, want 429 only on request %d", i, code, searchLimit+1)
		}
	}
}

func TestRateLimit_SpoofedAddressDoesNotSpendAnotherClientsAllowance(t *testing.T) {
	t.Parallel()
	h := newClientIPTestRouter(t, zap.NewNop(), Options{})

	const victim = "198.51.100.7"
	for i := 1; i <= searchLimit; i++ {
		code := get(t, h, "203.0.113.10:40000", map[string]string{
			"X-Forwarded-For": victim,
			"X-Real-IP":       victim,
			"True-Client-IP":  victim,
		})
		if code == http.StatusTooManyRequests {
			t.Fatalf("sender request %d limited early", i)
		}
	}

	if code := get(t, h, victim+":50000", nil); code == http.StatusTooManyRequests {
		t.Fatalf("first request from %s got 429: its allowance was spent by another peer", victim)
	}
	if code := get(t, h, "203.0.113.10:40001", nil); code != http.StatusTooManyRequests {
		t.Fatalf("sender's request %d: status %d, want 429", searchLimit+1, code)
	}
}

func TestAccessLog_RecordsPeerAddress(t *testing.T) {
	t.Parallel()
	core, logs := observer.New(zapcore.DebugLevel)
	h := newClientIPTestRouter(t, zap.New(core), Options{})

	get(t, h, "203.0.113.10:40000", map[string]string{
		"X-Forwarded-For": "192.0.2.66",
		"X-Real-IP":       "192.0.2.67",
		"True-Client-IP":  "192.0.2.68",
	})

	if got := loggedIP(t, logs); got != "203.0.113.10:40000" {
		t.Fatalf("access log ip = %q, want the peer address 203.0.113.10:40000", got)
	}
}

func TestRateLimit_TrustedProxyUsesRightmostUntrustedEntry(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	const proxy = "10.0.0.1:40000"

	t.Run("spoofed left-most entry is ignored", func(t *testing.T) {
		t.Parallel()
		h := newClientIPTestRouter(t, zap.NewNop(), Options{TrustedProxies: trusted})

		// The proxy chain appended 198.51.100.7 (the client it saw) and
		// 10.0.0.2 (an inner trusted hop); everything left of that came
		// from the client.
		for i := 1; i <= searchLimit+1; i++ {
			code := get(t, h, proxy, map[string]string{
				"X-Forwarded-For": fmt.Sprintf("192.0.2.%d, 198.51.100.7, 10.0.0.2", i),
			})
			if limited := code == http.StatusTooManyRequests; limited != (i > searchLimit) {
				t.Fatalf("request %d: status %d, want 429 only on request %d", i, code, searchLimit+1)
			}
		}

		// Another client behind the same proxy has its own allowance, even
		// when it claims the first client's spoofed address.
		code := get(t, h, proxy, map[string]string{"X-Forwarded-For": "192.0.2.1, 198.51.100.8"})
		if code == http.StatusTooManyRequests {
			t.Fatal("second client behind the proxy shares the first client's allowance")
		}
	})

	t.Run("other headers are ignored from a trusted proxy", func(t *testing.T) {
		t.Parallel()
		h := newClientIPTestRouter(t, zap.NewNop(), Options{TrustedProxies: trusted})

		for i := 1; i <= searchLimit+1; i++ {
			addr := fmt.Sprintf("192.0.2.%d", i)
			code := get(t, h, proxy, map[string]string{
				"X-Forwarded-For": "198.51.100.7",
				"X-Real-IP":       addr,
				"True-Client-IP":  addr,
			})
			if limited := code == http.StatusTooManyRequests; limited != (i > searchLimit) {
				t.Fatalf("request %d: status %d, want 429 only on request %d", i, code, searchLimit+1)
			}
		}
	})

	t.Run("untrusted peer cannot use the header", func(t *testing.T) {
		t.Parallel()
		h := newClientIPTestRouter(t, zap.NewNop(), Options{TrustedProxies: trusted})

		for i := 1; i <= searchLimit+1; i++ {
			code := get(t, h, "203.0.113.10:40000", map[string]string{
				"X-Forwarded-For": fmt.Sprintf("192.0.2.%d", i),
			})
			if limited := code == http.StatusTooManyRequests; limited != (i > searchLimit) {
				t.Fatalf("request %d: status %d, want 429 only on request %d", i, code, searchLimit+1)
			}
		}
	})

	t.Run("access log records the forwarded client", func(t *testing.T) {
		t.Parallel()
		core, logs := observer.New(zapcore.DebugLevel)
		h := newClientIPTestRouter(t, zap.New(core), Options{TrustedProxies: trusted})

		get(t, h, proxy, map[string]string{"X-Forwarded-For": "192.0.2.66, 198.51.100.7"})

		if got := loggedIP(t, logs); got != "198.51.100.7" {
			t.Fatalf("access log ip = %q, want 198.51.100.7", got)
		}
	})
}

// loggedIP returns the ip field of the most recent access-log entry.
func loggedIP(t *testing.T, logs *observer.ObservedLogs) string {
	t.Helper()
	all := logs.All()
	if len(all) == 0 {
		t.Fatal("no access log entry")
	}
	for _, f := range all[len(all)-1].Context {
		if f.Key == "ip" {
			return f.String
		}
	}
	t.Fatal("access log entry has no ip field")
	return ""
}
