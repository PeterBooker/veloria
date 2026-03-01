package repo

import (
	"context"
	"encoding/json"
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

// APIClient wraps the HTTP client, API key, and circuit breaker for
// AspireCloud API calls. Pass into StoreConfig to eliminate package-level globals.
type APIClient struct {
	apiKey  string
	breaker *gobreaker.CircuitBreaker[[]byte]
}

// NewAPIClient creates a new API client with the given API key and logger.
// The logger is used to report circuit breaker state transitions.
func NewAPIClient(apiKey string, l *zap.Logger) *APIClient {
	breaker := gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name:        "aspirecloud-api",
		MaxRequests: 3,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 5
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
	return &APIClient{
		apiKey:  apiKey,
		breaker: breaker,
	}
}

// BreakerState returns the current circuit breaker state name.
func (ac *APIClient) BreakerState() string {
	return ac.breaker.State().String()
}

// FetchJSON fetches JSON from a WordPress/AspireCloud API URL, using
// the circuit breaker and API key authentication.
func (ac *APIClient) FetchJSON(ctx context.Context, url string, out any) error {
	if out == nil {
		return fmt.Errorf("decode target is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	body, err := ac.breaker.Execute(func() ([]byte, error) {
		return ac.doRequest(ctx, url)
	})
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode JSON from %s: %w", url, err)
	}
	return nil
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
