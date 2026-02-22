package repo

import (
	"fmt"
	"io"
	"strings"
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
