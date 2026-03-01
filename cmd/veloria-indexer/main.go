package main

import (
	"encoding/json"
	"flag"
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

var (
	repo   = "plugins"
	zipURL string
	slug   string
)

func main() {
	flag.StringVar(&repo, "repo", "plugins", "Which repository to index: plugins|themes|cores")
	flag.StringVar(&zipURL, "zipurl", "", "URL to a zip file to download and extract before indexing (required)")
	flag.StringVar(&slug, "slug", "", "Destination folder name under source/ (required)")
	flag.Usage = func() {
		log.Printf("Usage: %s -repo=<plugins|themes|cores> -zipurl=<url> -slug=<name>\n", filepath.Base(os.Args[0])) // #nosec G706 -- os.Args[0] is the binary name, sanitized by filepath.Base
		flag.PrintDefaults()
	}
	flag.Parse()

	repo = strings.ToLower(repo)
	switch repo {
	case "plugins", "themes", "cores":
	default:
		log.Fatalf("invalid -repo %q; must be one of: plugins, themes, cores", repo)
	}
	if zipURL == "" || slug == "" {
		flag.Usage()
		log.Fatal("both -zipurl and -slug are required")
	}
	if strings.ContainsAny(slug, `/\`) {
		log.Fatalf("invalid -slug %q; must not contain path separators", slug)
	}

	c, err := config.New()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if err := c.EnsureDirs(); err != nil {
		log.Fatalf("failed to ensure data directories: %v", err)
	}
	base := filepath.Join(c.DataDir, repo)
	indexDir := filepath.Join(base, "index")
	sourceDir := filepath.Join(base, "source")

	// Download the zip to a temp file.
	tmpZip, cleanup, err := downloadZip(zipURL)
	if err != nil {
		log.Fatalf("failed to download zip: %v url: %s", err, zipURL)
	}
	defer cleanup()

	// Extract text files into source/<slug> and collect stats.
	dest := filepath.Join(sourceDir, slug)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		log.Fatalf("failed to create destination dir %q: %v", dest, err)
	}
	stats, err := index.UnzipTextFilesWithStats(tmpZip, dest)
	if err != nil {
		log.Fatalf("Failed to unzip text files into %q: %v", dest, err)
	}
	// stats are reported via EXTRACT_STATS structured output

	// Index to a temporary file, then atomically rename to final path.
	// This allows a single directory per slug instead of versioned directories.
	slugDir := filepath.Join(indexDir, slug)
	tmpPath := filepath.Join(slugDir, "trigrams.tmp")
	finalPath := filepath.Join(slugDir, "trigrams")

	if err := os.MkdirAll(slugDir, 0o750); err != nil {
		log.Fatalf("failed to create index dir %q: %v", slugDir, err)
	}

	// Remove any stale .tmp file from a previous failed run
	if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
		log.Printf("warning: failed to remove stale tmp file %q: %v", tmpPath, err)
	}

	index.IndexDirToFile(dest, tmpPath)

	// Atomically rename .tmp to final path
	if err := os.Rename(tmpPath, finalPath); err != nil {
		log.Fatalf("failed to rename %q to %q: %v", tmpPath, finalPath, err)
	}

	// Gzip-compress source files now that the trigram index has been built
	// from the uncompressed content. Compressed files reduce disk footprint
	// and page cache pressure during search.
	if err := index.CompressSourceDir(dest); err != nil {
		log.Fatalf("failed to compress source files in %q: %v", dest, err)
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
}

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

	client := client.GetZip()
	resp, err := client.Get(u)
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
		// Exit code 2 signals "download not found" to the calling server process,
		// replacing the fragile stderr "404" string match.
		log.Printf("download not found (404): %s", u)
		os.Exit(2)
	}
	if resp.StatusCode != http.StatusOK {
		cleanup()
		return "", func() {}, &downloadError{status: resp.Status}
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

type downloadError struct{ status string }

func (e *downloadError) Error() string { return "unexpected HTTP status: " + e.status }
