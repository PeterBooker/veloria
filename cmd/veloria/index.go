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
	"veloria/internal/repo"
)

const (
	downloadMaxRetries   = 3
	downloadMaxRetryWait = 60 * time.Second
)

// downloadDefaultRetryWait is the wait between attempts when the upstream
// sends no Retry-After header. A variable so tests can shorten it.
var downloadDefaultRetryWait = 10 * time.Second

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

	dest := filepath.Join(sourceDir, c.Slug)

	// A file lock rather than in-process locking: an indexer orphaned by a
	// server crash may still be writing to this slug's directories.
	releaseLock, err := acquireSlugLock(dest + ".lock")
	if err != nil {
		return fmt.Errorf("failed to lock slug %q: %w", c.Slug, err)
	}
	defer releaseLock()

	// Download the zip to a temp file.
	tmpZip, cleanup, err := downloadZip(c.ZipURL)
	if err != nil {
		return fmt.Errorf("failed to download zip: %w url: %s", err, c.ZipURL)
	}
	defer cleanup()

	// Extract to a staging directory so the existing source files remain
	// available for in-flight searches. The staging dir is atomically renamed
	// to the final path once the index and compression are complete.
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
//
// The outcome is signalled to the parent server process through the exit
// code carried by exitError:
//   - repo.ExitDownloadNotFound (400, 403, 404, 410, or a body that is not a
//     zip): the URL will never work, so the caller may fall back to another
//     URL or close the extension.
//   - repo.ExitDownloadUnavailable (5xx or a transport failure on every
//     attempt): the URL may work later, so the caller may fall back to
//     another URL and should retry rather than close.
//   - a plain error for 429 on every attempt, retried on the caller's next cycle.
//
// Between attempts it waits for the Retry-After header when present,
// otherwise downloadDefaultRetryWait, capped at downloadMaxRetryWait.
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
	fail := func(err error) (string, func(), error) {
		cleanup()
		return "", func() {}, err
	}

	c := client.GetZip()

	var (
		lastErr   error
		transient bool // the last failure was a 5xx or transport error rather than a 429
	)
	for attempt := 1; attempt <= downloadMaxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return fail(err)
		}
		req.Header.Set("User-Agent", client.UserAgent)
		// GitHub API release asset URLs require this header to serve the
		// binary content instead of JSON metadata.
		if isGitHubReleaseAsset(u) {
			req.Header.Set("Accept", "application/octet-stream")
		}

		resp, err := c.Do(req)
		if err != nil {
			lastErr, transient = err, true
			waitBeforeRetry(attempt, "", err.Error())
			continue
		}

		switch {
		case isPermanentHTTPFailure(resp.StatusCode):
			_ = resp.Body.Close()
			return fail(&exitError{code: repo.ExitDownloadNotFound, msg: fmt.Sprintf("download unavailable (%d)", resp.StatusCode)})
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			retryAfter := resp.Header.Get("Retry-After")
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("unexpected HTTP status: %s", resp.Status)
			transient = resp.StatusCode >= 500
			waitBeforeRetry(attempt, retryAfter, resp.Status)
			continue
		case resp.StatusCode != http.StatusOK:
			_ = resp.Body.Close()
			return fail(fmt.Errorf("unexpected HTTP status: %s", resp.Status))
		}

		_, err = io.Copy(tmpFile, resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			// A partial body is worthless; discard it and fetch again.
			if terr := truncateFile(tmpFile); terr != nil {
				return fail(terr)
			}
			lastErr, transient = err, true
			waitBeforeRetry(attempt, "", err.Error())
			continue
		}
		if err := tmpFile.Close(); err != nil {
			return fail(err)
		}

		// Validate the file is actually a zip by checking the magic bytes (PK\x03\x04).
		// Some servers return HTML error pages with a 200 status, which would otherwise
		// cause a confusing "not a valid zip file" error during extraction.
		if err := validateZipMagic(tmpPath); err != nil {
			return fail(&exitError{code: repo.ExitDownloadNotFound, msg: fmt.Sprintf("downloaded file is not a valid zip: %v", err)})
		}
		return tmpPath, cleanup, nil
	}

	cleanup()
	if transient {
		return "", func() {}, &exitError{
			code: repo.ExitDownloadUnavailable,
			msg:  fmt.Sprintf("download failed on all %d attempts: %v", downloadMaxRetries, lastErr),
		}
	}
	return "", func() {}, lastErr
}

// waitBeforeRetry sleeps before the next download attempt, if there is one.
func waitBeforeRetry(attempt int, retryAfter, reason string) {
	if attempt >= downloadMaxRetries {
		return
	}
	wait := parseRetryAfterHeader(retryAfter)
	log.Printf("download attempt %d/%d failed (%s), retrying in %s", attempt, downloadMaxRetries, reason, wait)
	time.Sleep(wait)
}

// truncateFile discards everything written to f so the next attempt starts
// from an empty file.
func truncateFile(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.Seek(0, io.SeekStart)
	return err
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
