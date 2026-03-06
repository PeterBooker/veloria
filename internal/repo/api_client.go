package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	gobreaker "github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"veloria/internal/client"
	"veloria/internal/telemetry"
)

// ThrottleConfig holds outbound request throttling settings.
type ThrottleConfig struct {
	RequestsPerSecond float64       // sustained rate; 0 = unlimited
	Burst             int           // max burst size
	MaxRetries        int           // max 429 retries per request
	DefaultRetryDelay time.Duration // fallback when Retry-After header is absent
}

// APIClient wraps the HTTP client, API key, circuit breaker, and rate limiter
// for AspireCloud API calls. Pass into StoreConfig to eliminate package-level globals.
type APIClient struct {
	apiKey            string
	breaker           *gobreaker.CircuitBreaker[[]byte]
	logger            *zap.Logger
	throttle          chan time.Time // token bucket for rate limiting; nil = unlimited
	done              chan struct{}  // closed on Close() to stop the refill goroutine
	maxRetries        int
	defaultRetryDelay time.Duration
}

// NewAPIClient creates a new API client with the given API key, logger, and
// throttle configuration. The logger is used to report circuit breaker state
// transitions and 429 retries.
func NewAPIClient(apiKey string, l *zap.Logger, tc ThrottleConfig) *APIClient {
	breaker := gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name:        "aspirecloud-api",
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 5
		},
		// Treat 429 responses as non-failures so they don't trip the breaker.
		// Returning true means the error is counted as a success.
		// Note: gobreaker calls IsSuccessful for ALL results, including nil errors,
		// so we must return true for nil (actual success) as well.
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			var throttled *ErrThrottled
			return errors.As(err, &throttled)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			l.Warn("Circuit breaker state change",
				zap.String("name", name),
				zap.String("from", from.String()),
				zap.String("to", to.String()),
			)
			if telemetry.CircuitBreakerChanges != nil {
				telemetry.CircuitBreakerChanges.Add(context.Background(), 1,
					metric.WithAttributes(
						attribute.String("name", name),
						attribute.String("to_state", to.String()),
					),
				)
			}
		},
	})

	ac := &APIClient{
		apiKey:            apiKey,
		breaker:           breaker,
		logger:            l,
		maxRetries:        tc.MaxRetries,
		defaultRetryDelay: tc.DefaultRetryDelay,
	}

	// Set up channel-based throttle if rate > 0.
	if tc.RequestsPerSecond > 0 {
		burst := max(tc.Burst, 1)
		ac.throttle = make(chan time.Time, burst)
		ac.done = make(chan struct{})

		// Pre-fill burst tokens.
		for range burst {
			ac.throttle <- time.Now()
		}

		// Refill goroutine.
		interval := time.Duration(float64(time.Second) / tc.RequestsPerSecond)
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case t := <-ticker.C:
					select {
					case ac.throttle <- t:
					default: // bucket full, discard
					}
				case <-ac.done:
					return
				}
			}
		}()
	}

	return ac
}

// Close stops the rate limiter refill goroutine. Safe to call multiple times.
func (ac *APIClient) Close() {
	if ac.done != nil {
		select {
		case <-ac.done:
			// already closed
		default:
			close(ac.done)
		}
	}
}

// BreakerState returns the current circuit breaker state name.
func (ac *APIClient) BreakerState() string {
	return ac.breaker.State().String()
}

// waitForToken blocks until a rate limit token is available or ctx is cancelled.
func (ac *APIClient) waitForToken(ctx context.Context) error {
	if ac.throttle == nil {
		return nil
	}
	select {
	case <-ac.throttle:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// FetchJSON fetches JSON from a WordPress/AspireCloud API URL, using
// the rate limiter, circuit breaker, and API key authentication.
// On 429 responses, it respects the Retry-After header and retries
// up to maxRetries times.
func (ac *APIClient) FetchJSON(ctx context.Context, url string, out any) error {
	if out == nil {
		return fmt.Errorf("decode target is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Wait for a rate limit token before making the request.
	if err := ac.waitForToken(ctx); err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= ac.maxRetries; attempt++ {
		body, err := ac.breaker.Execute(func() ([]byte, error) {
			return ac.doRequest(ctx, url)
		})
		if err != nil {
			var throttled *ErrThrottled
			if errors.As(err, &throttled) {
				ac.logger.Warn("Received 429 from API, backing off",
					zap.String("url", url),
					zap.Duration("retry_after", throttled.RetryAfter),
					zap.Int("attempt", attempt+1),
					zap.Int("max_retries", ac.maxRetries),
				)
				lastErr = err

				if attempt >= ac.maxRetries {
					break
				}

				// Sleep for the Retry-After duration.
				select {
				case <-time.After(throttled.RetryAfter):
				case <-ctx.Done():
					return ctx.Err()
				}

				// Re-acquire a rate limit token before retrying.
				if err := ac.waitForToken(ctx); err != nil {
					return err
				}
				continue
			}
			return err
		}

		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("failed to decode JSON from %s: %w", url, err)
		}
		return nil
	}

	return fmt.Errorf("exhausted %d retries for %s: %w", ac.maxRetries, url, lastErr)
}

func (ac *APIClient) doRequest(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", client.UserAgent)
	req.Header.Set("Accept", "application/json")
	if ac.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+ac.apiKey)
	}

	httpClient := client.GetAPI()
	resp, err := httpClient.Do(req) // #nosec G704 -- URL from internal constants, not user input
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := resp.Body.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), ac.defaultRetryDelay)
		snippet, _ := readBodySnippet(resp.Body, wpAPIErrorMaxBytes)
		return nil, &ErrThrottled{
			RetryAfter: retryAfter,
			Err: &wpAPIError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				URL:        url,
				Body:       snippet,
			},
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := readBodySnippet(resp.Body, wpAPIErrorMaxBytes)
		return nil, &wpAPIError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			URL:        url,
			Body:       snippet,
		}
	}

	body, err := readBodyWithLimit(resp.Body, wpAPIResponseMaxBytes)
	if err != nil {
		return nil, err
	}
	return body, nil
}
