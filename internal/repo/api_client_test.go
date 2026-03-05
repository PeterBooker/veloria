package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

// newTestAPIClient creates an APIClient that talks to the given test server.
// It patches the default HTTP client transport so requests go to the test server.
func newTestAPIClient(t *testing.T, tc ThrottleConfig) *APIClient {
	t.Helper()
	ac := NewAPIClient("", testLogger(), tc)
	t.Cleanup(ac.Close)
	return ac
}

// --- parseRetryAfter tests ---

func TestParseRetryAfter_Seconds(t *testing.T) {
	d := parseRetryAfter("120", 5*time.Second)
	assert.Equal(t, 120*time.Second, d)
}

func TestParseRetryAfter_Empty(t *testing.T) {
	d := parseRetryAfter("", 5*time.Second)
	assert.Equal(t, 5*time.Second, d)
}

func TestParseRetryAfter_Capped(t *testing.T) {
	d := parseRetryAfter("600", 5*time.Second)
	assert.Equal(t, 5*time.Minute, d)
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	future := time.Now().Add(30 * time.Second).UTC().Format(http.TimeFormat)
	d := parseRetryAfter(future, 5*time.Second)
	// Should be roughly 30 seconds (allow some slack for test execution time).
	assert.InDelta(t, 30*time.Second, d, float64(2*time.Second))
}

func TestParseRetryAfter_InvalidFallsBack(t *testing.T) {
	d := parseRetryAfter("not-a-number-or-date", 7*time.Second)
	assert.Equal(t, 7*time.Second, d)
}

func TestParseRetryAfter_ZeroSeconds(t *testing.T) {
	d := parseRetryAfter("0", 5*time.Second)
	assert.Equal(t, 5*time.Second, d)
}

func TestParseRetryAfter_NegativeSeconds(t *testing.T) {
	d := parseRetryAfter("-10", 5*time.Second)
	assert.Equal(t, 5*time.Second, d)
}

// --- Throttle tests ---

func TestThrottle_Disabled(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 0, // disabled
		Burst:             1,
		MaxRetries:        0,
		DefaultRetryDelay: time.Second,
	})

	start := time.Now()
	for i := 0; i < 5; i++ {
		var out map[string]string
		err := ac.FetchJSON(context.Background(), srv.URL, &out)
		require.NoError(t, err)
	}
	elapsed := time.Since(start)

	assert.Equal(t, int32(5), calls.Load())
	// With throttling disabled, all 5 requests should complete very quickly.
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestThrottle_RateLimiting(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 5, // 1 token every 200ms
		Burst:             1, // only 1 immediate
		MaxRetries:        0,
		DefaultRetryDelay: time.Second,
	})

	start := time.Now()
	for i := 0; i < 4; i++ {
		var out map[string]string
		err := ac.FetchJSON(context.Background(), srv.URL, &out)
		require.NoError(t, err)
	}
	elapsed := time.Since(start)

	assert.Equal(t, int32(4), calls.Load())
	// 1 immediate (burst), then 3 more at ~200ms each = ~600ms minimum.
	assert.GreaterOrEqual(t, elapsed, 500*time.Millisecond)
}

func TestThrottle_ContextCancelled(t *testing.T) {
	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 0.1, // very slow: 1 token every 10s
		Burst:             1,
		MaxRetries:        0,
		DefaultRetryDelay: time.Second,
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer srv.Close()

	// First request consumes the burst token.
	var out map[string]string
	err := ac.FetchJSON(context.Background(), srv.URL, &out)
	require.NoError(t, err)

	// Second request should block waiting for a token. Cancel the context.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = ac.FetchJSON(ctx, srv.URL, &out)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// --- 429 retry tests ---

func Test429_RetrySuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 0, // no throttle for speed
		Burst:             1,
		MaxRetries:        3,
		DefaultRetryDelay: time.Second,
	})

	start := time.Now()
	var out map[string]string
	err := ac.FetchJSON(context.Background(), srv.URL, &out)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "ok", out["status"])
	assert.Equal(t, int32(3), calls.Load())
	// Two retries with Retry-After: 1 each = ~2s minimum.
	assert.GreaterOrEqual(t, elapsed, 1800*time.Millisecond)
}

func Test429_ExhaustsRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 0,
		Burst:             1,
		MaxRetries:        2,
		DefaultRetryDelay: time.Second,
	})

	var out map[string]string
	err := ac.FetchJSON(context.Background(), srv.URL, &out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exhausted 2 retries")
	// Initial attempt + 2 retries = 3 calls.
	assert.Equal(t, int32(3), calls.Load())
}

func Test429_DoesNotTripCircuitBreaker(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		// Return 429 for the first 8 calls (enough to trip the breaker if counted as failures).
		if n <= 8 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 0,
		Burst:             1,
		MaxRetries:        0, // no retries per request — each call gets one attempt
		DefaultRetryDelay: 100 * time.Millisecond,
	})

	// Make 8 requests that all get 429. These should NOT trip the breaker
	// (breaker trips at >5 consecutive failures).
	for i := 0; i < 8; i++ {
		var out map[string]string
		err := ac.FetchJSON(context.Background(), srv.URL, &out)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "throttled")
	}

	// The breaker should still be closed.
	assert.Equal(t, "closed", ac.BreakerState())

	// The 9th request should succeed (server returns 200).
	var out map[string]string
	err := ac.FetchJSON(context.Background(), srv.URL, &out)
	require.NoError(t, err)
	assert.Equal(t, "ok", out["status"])
}

func Test429_RespectsDefaultRetryDelay(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 1 {
			// No Retry-After header — should use default delay.
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 0,
		Burst:             1,
		MaxRetries:        1,
		DefaultRetryDelay: 500 * time.Millisecond,
	})

	start := time.Now()
	var out map[string]string
	err := ac.FetchJSON(context.Background(), srv.URL, &out)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "ok", out["status"])
	assert.GreaterOrEqual(t, elapsed, 400*time.Millisecond)
}

// --- ErrThrottled type tests ---

func TestErrThrottled_Unwrap(t *testing.T) {
	inner := &wpAPIError{StatusCode: 429, Status: "429 Too Many Requests", URL: "http://example.com"}
	err := &ErrThrottled{RetryAfter: 5 * time.Second, Err: inner}

	var apiErr *wpAPIError
	assert.True(t, assert.ErrorAs(t, err, &apiErr))
	assert.Equal(t, 429, apiErr.StatusCode)
}

func TestErrThrottled_ErrorMessage(t *testing.T) {
	inner := &wpAPIError{StatusCode: 429, Status: "429 Too Many Requests", URL: "http://example.com"}
	err := &ErrThrottled{RetryAfter: 5 * time.Second, Err: inner}

	msg := err.Error()
	assert.Contains(t, msg, "throttled")
	assert.Contains(t, msg, "5s")
	assert.Contains(t, msg, "429 Too Many Requests")
}

// --- Non-429 errors still trip breaker ---

func TestNon429Error_TripsCircuitBreaker(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	ac := newTestAPIClient(t, ThrottleConfig{
		RequestsPerSecond: 0,
		Burst:             1,
		MaxRetries:        0,
		DefaultRetryDelay: time.Second,
	})

	// Make 8 requests that all get 500. Breaker trips at >5 consecutive failures
	// (i.e. on the 6th failure), so requests 7+ are blocked by the open breaker.
	for i := 0; i < 8; i++ {
		var out map[string]string
		_ = ac.FetchJSON(context.Background(), srv.URL, &out)
	}

	assert.Equal(t, "open", ac.BreakerState())
	// Only 6 requests reached the server; the breaker opened on the 6th failure
	// and blocked subsequent attempts.
	assert.Equal(t, int32(6), calls.Load(),
		fmt.Sprintf("expected 6 calls (breaker trips at >5), got %d", calls.Load()))
}
