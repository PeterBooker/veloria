package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"veloria/internal/client"
	"veloria/internal/config"
	"veloria/internal/index"
)

const (
	downloadMaxRetries       = 3
	downloadDefaultRetryWait = 10 * time.Second
	downloadMaxRetryWait     = 60 * time.Second
)

// IndexCmd downloads a zip, extracts text files, and builds a trigram search index.
// This command is invoked as a subprocess by the server to maintain process isolation.
type IndexCmd struct {
	Repo   string `name:"repo" help:"Repository type." enum:"plugins,themes,cores" default:"plugins"`
	ZipURL string `name:"zipurl" help:"URL of zip file to download and index." required:""`
	Slug   string `name:"slug" help:"Destination folder name under source/." required:""`
}

func (c *IndexCmd) Validate() error {
	if strings.ContainsAny(c.Slug, `/\`) {
		return fmt.Errorf("invalid slug %q: must not contain path separators", c.Slug)
	}
	return nil
}

func (c *IndexCmd) Run() error {
	cfg, err := config.New()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("failed to ensure data directories: %w", err)
	}
	base := filepath.Join(cfg.DataDir, c.Repo)
	indexDir := filepath.Join(base, "index")
	sourceDir := filepath.Join(base, "source")

	// Download the zip to a temp file.
	tmpZip, cleanup, err := downloadZip(c.ZipURL)
	if err != nil {
		return fmt.Errorf("failed to download zip: %w url: %s", err, c.ZipURL)
	}
	defer cleanup()

	// Extract to a staging directory so the existing source files remain
	// available for in-flight searches. The staging dir is atomically renamed
	// to the final path once the index and compression are complete.
	dest := filepath.Join(sourceDir, c.Slug)
	stagingDest := dest + ".staging"
	if err := os.RemoveAll(stagingDest); err != nil {
		return fmt.Errorf("failed to remove stale staging dir %q: %w", stagingDest, err)
	}
	if err := os.MkdirAll(stagingDest, 0o750); err != nil {
		return fmt.Errorf("failed to create staging dir %q: %w", stagingDest, err)
	}
	// stagingSwapped tracks whether the staging dir was successfully renamed
	// to the final path. If not, the deferred cleanup removes it.
	stagingSwapped := false
	defer func() {
		if !stagingSwapped {
			_ = os.RemoveAll(stagingDest)
		}
	}()
	stats, err := index.UnzipWithStats(tmpZip, stagingDest)
	if err != nil {
		return fmt.Errorf("failed to unzip files into %q: %w", stagingDest, err)
	}

	// Build the trigram index from the staging directory. Paths stored in the
	// index use the final dest so the index is valid immediately after rename.
	slugDir := filepath.Join(indexDir, c.Slug)
	tmpPath := filepath.Join(slugDir, "trigrams.tmp")
	finalPath := filepath.Join(slugDir, "trigrams")

	if err := os.MkdirAll(slugDir, 0o750); err != nil {
		return fmt.Errorf("failed to create index dir %q: %w", slugDir, err)
	}

	// Remove any stale .tmp file from a previous failed run
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		log.Printf("warning: failed to remove stale tmp file %q: %v", tmpPath, err)
	}

	index.IndexDirToFile(stagingDest, tmpPath, dest)
	defer func() {
		// Clean up trigrams.tmp if it was not renamed to the final path.
		if _, err := os.Stat(tmpPath); err == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	// Gzip-compress source files before swapping into place.
	if err := index.CompressSourceDir(stagingDest); err != nil {
		return fmt.Errorf("failed to compress source files in %q: %w", stagingDest, err)
	}

	// Atomic swap: move old source out of the way, move staging into place,
	// then rename the trigram index. This keeps old source files available
	// until the very last moment so concurrent searches are not disrupted.
	oldDest := dest + ".old"
	_ = os.RemoveAll(oldDest)
	// Rename existing source dir (may fail if this is the first index — that's OK).
	_ = os.Rename(dest, oldDest)
	if err := os.Rename(stagingDest, dest); err != nil {
		// Try to restore the old source dir on failure.
		_ = os.Rename(oldDest, dest)
		return fmt.Errorf("failed to rename staging dir %q to %q: %w", stagingDest, dest, err)
	}
	stagingSwapped = true
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to rename %q to %q: %w", tmpPath, finalPath, err)
	}
	// Clean up old source directory synchronously. A background goroutine
	// would race with subprocess exit and likely never complete.
	_ = os.RemoveAll(oldDest)

	// Output the index directory path so the server can load it.
	// Format: INDEX_READY:<path>
	fmt.Printf("INDEX_READY:%s\n", slugDir)

	// Output extraction stats as JSON.
	// Format: EXTRACT_STATS:<json>
	statsJSON, err := json.Marshal(stats)
	if err != nil {
		log.Printf("warning: failed to marshal stats: %v", err)
	} else {
		fmt.Printf("EXTRACT_STATS:%s\n", statsJSON)
	}

	return nil
}

// downloadZip fetches a zip file from the given URL into a temporary file.
// It returns the temp file path, a cleanup function, and any error.
// On permanently-failing HTTP statuses (400, 403, 404, 410), it returns an
// exitError with code 2 to signal "download unavailable" to the calling
// server process, preventing futile retries.
// On 429 (Too Many Requests), it respects the Retry-After header and retries
// up to downloadMaxRetries times.
func downloadZip(u string) (string, func(), error) {
	tmpFile, err := os.CreateTemp("", "download-*.zip")
	if err != nil {
		return "", func() {}, err
	}
	tmpPath := tmpFile.Name()
	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}

	c := client.GetZip()

	var lastErr error
	for attempt := range downloadMaxRetries {
		req, reqErr := http.NewRequest(http.MethodGet, u, nil)
		if reqErr != nil {
			cleanup()
			return "", func() {}, reqErr
		}
		req.Header.Set("User-Agent", client.UserAgent)
		// GitHub API release asset URLs require this header to serve the
		// binary content instead of JSON metadata.
		if isGitHubReleaseAsset(u) {
			req.Header.Set("Accept", "application/octet-stream")
		}

		var resp *http.Response
		resp, err = c.Do(req)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}

		if isPermanentHTTPFailure(resp.StatusCode) {
			_ = resp.Body.Close()
			cleanup()
			return "", func() {}, &exitError{code: 2, msg: fmt.Sprintf("download unavailable (%d): %s", resp.StatusCode, u)}
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := parseRetryAfterHeader(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("unexpected HTTP status: %s", resp.Status)
			if attempt < downloadMaxRetries-1 {
				log.Printf("rate limited (429), waiting %s before retry %d/%d", wait, attempt+2, downloadMaxRetries)
				time.Sleep(wait)
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			cleanup()
			return "", func() {}, fmt.Errorf("unexpected HTTP status: %s url: %s", resp.Status, u)
		}

		if _, err := io.Copy(tmpFile, resp.Body); err != nil {
			_ = resp.Body.Close()
			cleanup()
			return "", func() {}, err
		}
		_ = resp.Body.Close()
		if err := tmpFile.Close(); err != nil {
			cleanup()
			return "", func() {}, err
		}

		// Validate the file is actually a zip by checking the magic bytes (PK\x03\x04).
		// Some servers return HTML error pages with a 200 status, which would otherwise
		// cause a confusing "not a valid zip file" error during extraction.
		if err := validateZipMagic(tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return "", func() {}, &exitError{code: 2, msg: fmt.Sprintf("downloaded file is not a valid zip: %s", u)}
		}

		return tmpPath, cleanup, nil
	}

	// All retries exhausted (only reachable via 429 loop).
	cleanup()
	return "", func() {}, lastErr
}

// validateZipMagic checks that the file starts with the zip magic bytes (PK\x03\x04).
func validateZipMagic(path string) error {
	f, err := os.Open(path) // #nosec G304 -- path is always a temp file we just created via os.CreateTemp
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("file too small to be a zip")
	}
	if magic != [4]byte{'P', 'K', 0x03, 0x04} {
		return fmt.Errorf("missing zip magic bytes")
	}
	return nil
}

// isGitHubReleaseAsset returns true if the URL points to a GitHub API release asset.
// These URLs require Accept: application/octet-stream to download the binary.
func isGitHubReleaseAsset(u string) bool {
	return strings.Contains(u, "api.github.com/") && strings.Contains(u, "/releases/assets/")
}

// parseRetryAfterHeader parses a Retry-After header value (delay-seconds format).
// Returns downloadDefaultRetryWait if the header is empty or unparseable,
// capped at downloadMaxRetryWait.
func parseRetryAfterHeader(val string) time.Duration {
	val = strings.TrimSpace(val)
	if val == "" {
		return downloadDefaultRetryWait
	}
	seconds, err := strconv.Atoi(val)
	if err != nil || seconds <= 0 {
		return downloadDefaultRetryWait
	}
	d := time.Duration(seconds) * time.Second
	if d > downloadMaxRetryWait {
		return downloadMaxRetryWait
	}
	return d
}

// isPermanentHTTPFailure returns true for HTTP status codes that indicate the
// download will never succeed and should not be retried.
func isPermanentHTTPFailure(code int) bool {
	switch code {
	case http.StatusBadRequest, // 400
		http.StatusForbidden, // 403
		http.StatusNotFound,  // 404
		http.StatusGone:      // 410
		return true
	}
	return false
}

// exitError is an error that carries a process exit code.
// Kong's FatalIfErrorf uses the ExitCode() method to determine the exit code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }
func (e *exitError) ExitCode() int { return e.code }
