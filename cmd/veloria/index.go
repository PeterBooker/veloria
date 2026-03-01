package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"veloria/internal/client"
	"veloria/internal/config"
	"veloria/internal/index"
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

	// Extract text files into source/<slug> and collect stats.
	dest := filepath.Join(sourceDir, c.Slug)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fmt.Errorf("failed to create destination dir %q: %w", dest, err)
	}
	stats, err := index.UnzipTextFilesWithStats(tmpZip, dest)
	if err != nil {
		return fmt.Errorf("failed to unzip text files into %q: %w", dest, err)
	}

	// Index to a temporary file, then atomically rename to final path.
	// This allows a single directory per slug instead of versioned directories.
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

	index.IndexDirToFile(dest, tmpPath)

	// Atomically rename .tmp to final path
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("failed to rename %q to %q: %w", tmpPath, finalPath, err)
	}

	// Gzip-compress source files now that the trigram index has been built
	// from the uncompressed content. Compressed files reduce disk footprint
	// and page cache pressure during search.
	if err := index.CompressSourceDir(dest); err != nil {
		return fmt.Errorf("failed to compress source files in %q: %w", dest, err)
	}

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
// On HTTP 404, it returns an exitError with code 2 to signal "download not found"
// to the calling server process.
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
	resp, err := c.Get(u)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		cleanup()
		// Exit code 2 signals "download not found" to the calling server process.
		return "", func() {}, &exitError{code: 2, msg: fmt.Sprintf("download not found (404): %s", u)}
	}
	if resp.StatusCode != http.StatusOK {
		cleanup()
		return "", func() {}, fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmpPath, cleanup, nil
}

// exitError is an error that carries a process exit code.
// Kong's FatalIfErrorf uses the ExitCode() method to determine the exit code.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string  { return e.msg }
func (e *exitError) ExitCode() int  { return e.code }
