package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gobreaker "github.com/sony/gobreaker/v2"

	"veloria/internal/client"
)

const (
	wpAPIResponseMaxBytes = 10 << 20 // 10 MiB
	wpAPIErrorMaxBytes    = 4 << 10  // 4 KiB
)

// wpAPIError represents an HTTP error response from the WordPress API.
type wpAPIError struct {
	StatusCode int
	Status     string
	URL        string
	Body       string
}

func (e *wpAPIError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("unexpected status %s for %s: %s", e.Status, e.URL, e.Body)
	}
	return fmt.Sprintf("unexpected status %s for %s", e.Status, e.URL)
}

// aspireAPIKey holds the API key for AspireCloud requests.
// Set via SetAPIKey during application startup.
var aspireAPIKey string

// SetAPIKey sets the API key for AspireCloud API requests.
func SetAPIKey(key string) { aspireAPIKey = key }

// wpAPIBreaker protects against cascading failures when the API is down.
var wpAPIBreaker = gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
	Name:        "aspirecloud-api",
	MaxRequests: 3,
	Interval:    60 * time.Second,
	Timeout:     30 * time.Second,
	ReadyToTrip: func(counts gobreaker.Counts) bool {
		return counts.ConsecutiveFailures > 5
	},
})

func fetchWPAPIJSON(ctx context.Context, url string, out any) (err error) {
	if out == nil {
		return fmt.Errorf("decode target is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	body, err := wpAPIBreaker.Execute(func() ([]byte, error) {
		return doWPAPIRequest(ctx, url)
	})
	if err != nil {
		return err
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode JSON from %s: %w", url, err)
	}
	return nil
}

func doWPAPIRequest(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", client.UserAgent)
	req.Header.Set("Accept", "application/json")
	if aspireAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+aspireAPIKey)
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

func readBodyWithLimit(r io.Reader, maxBytes int64) ([]byte, error) {
	lr := &io.LimitedReader{R: r, N: maxBytes + 1}
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("response body too large (>%d bytes)", maxBytes)
	}
	return b, nil
}

func readBodySnippet(r io.Reader, maxBytes int64) (string, error) {
	lr := &io.LimitedReader{R: r, N: maxBytes}
	b, err := io.ReadAll(lr)
	if err != nil {
		return "", err
	}

	snippet := strings.TrimSpace(string(b))
	snippet = strings.Join(strings.Fields(snippet), " ")
	return snippet, nil
}
