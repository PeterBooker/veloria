package repo

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
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

// ErrThrottled represents a 429 Too Many Requests response.
// It wraps a wpAPIError and carries the parsed Retry-After duration.
type ErrThrottled struct {
	RetryAfter time.Duration
	Err        *wpAPIError
}

func (e *ErrThrottled) Error() string {
	return fmt.Sprintf("throttled (retry after %s): %s", e.RetryAfter, e.Err.Error())
}

func (e *ErrThrottled) Unwrap() error {
	return e.Err
}

// parseRetryAfter parses the Retry-After header value.
// It handles both delay-seconds (e.g. "120") and HTTP-date formats.
// Returns defaultDelay if the header is empty or unparseable.
// The result is capped at 5 minutes to avoid absurd waits.
func parseRetryAfter(headerVal string, defaultDelay time.Duration) time.Duration {
	const maxDelay = 5 * time.Minute

	headerVal = strings.TrimSpace(headerVal)
	if headerVal == "" {
		return defaultDelay
	}

	// Try seconds first (most common for APIs).
	if seconds, err := strconv.Atoi(headerVal); err == nil && seconds > 0 {
		d := time.Duration(seconds) * time.Second
		if d > maxDelay {
			return maxDelay
		}
		return d
	}

	// Try HTTP-date format (RFC 7231).
	if t, err := http.ParseTime(headerVal); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return defaultDelay
		}
		if d > maxDelay {
			return maxDelay
		}
		return d
	}

	return defaultDelay
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
