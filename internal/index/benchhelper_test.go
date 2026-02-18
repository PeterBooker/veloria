package index

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const pluginZipURL = "https://downloads.wordpress.org/plugin/woocommerce.10.5.1.zip"

// pluginZipPath returns the cached zip path under the project-root testdata/ directory.
func pluginZipPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(projectRoot, "testdata", "plugins", "woocommerce.10.5.1.zip")
}

// ensurePluginZip downloads the plugin zip if not already cached. Returns the path.
// Skips the benchmark if the download fails (e.g. no network).
func ensurePluginZip(b *testing.B) string {
	b.Helper()

	zipPath := pluginZipPath()
	if _, err := os.Stat(zipPath); err == nil {
		return zipPath
	}

	b.Logf("downloading %s ...", pluginZipURL)

	if err := os.MkdirAll(filepath.Dir(zipPath), 0o750); err != nil {
		b.Fatal(err)
	}

	resp, err := http.Get(pluginZipURL) // #nosec G107 -- hardcoded test URL
	if err != nil {
		b.Skipf("failed to download plugin zip (no network?): %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b.Skipf("failed to download plugin zip: HTTP %d", resp.StatusCode)
	}

	tmp := zipPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		b.Fatal(err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		b.Skipf("failed to download plugin zip: %v", err)
	}
	_ = f.Close()

	if err := os.Rename(tmp, zipPath); err != nil {
		_ = os.Remove(tmp)
		b.Fatal(err)
	}

	b.Logf("cached plugin zip at %s", zipPath)
	return zipPath
}

// buildPluginIndex extracts the WooCommerce plugin zip, builds a trigram index,
// optionally compresses the source files, and returns an opened *Index.
//
// The directory layout matches the production structure expected by SourceDir():
//
//	<tmpdir>/repo/source/woocommerce/  (extracted text files)
//	<tmpdir>/repo/index/woocommerce/   (trigram index)
func buildPluginIndex(b *testing.B, compress bool) *Index {
	b.Helper()

	zipPath := ensurePluginZip(b)

	dir := b.TempDir()
	sourceDir := filepath.Join(dir, "repo", "source", "woocommerce")
	indexDir := filepath.Join(dir, "repo", "index", "woocommerce")

	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(indexDir, 0o750); err != nil {
		b.Fatal(err)
	}

	if err := UnzipTextFiles(zipPath, sourceDir); err != nil {
		b.Fatalf("failed to extract plugin zip: %v", err)
	}

	trigramsPath := filepath.Join(indexDir, "trigrams")
	IndexDirToFile(sourceDir, trigramsPath)

	if compress {
		if err := CompressSourceDir(sourceDir); err != nil {
			b.Fatal(err)
		}
	}

	idx := Open(indexDir)
	if idx == nil {
		b.Fatal("failed to open built index")
	}

	b.Cleanup(func() { idx.Close() })
	return idx
}

// largestSourceFile returns the path of the largest source file in the index's source directory.
func largestSourceFile(b *testing.B, idx *Index) string {
	b.Helper()
	srcDir := idx.SourceDir()

	var bestPath string
	var bestSize int64

	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			bestPath = path
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	if bestPath == "" {
		b.Fatal("no source files found")
	}

	b.Logf("using %s (%d bytes)", filepath.Base(bestPath), bestSize)
	return bestPath
}

// sourceFileSize returns the uncompressed size of the given source file.
func sourceFileSize(b *testing.B, path string) int64 {
	b.Helper()
	data, err := readSourceFile(path)
	if err != nil {
		b.Fatal(err)
	}
	return int64(len(data))
}

// countSourceFiles returns the number of files in the source directory.
func countSourceFiles(b *testing.B, idx *Index) int {
	b.Helper()
	count := 0
	err := filepath.WalkDir(idx.SourceDir(), func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return count
}

// logIndexStats logs stats about the built index for benchmark context.
func logIndexStats(b *testing.B, idx *Index) {
	b.Helper()
	b.Logf("index: %d source files", countSourceFiles(b, idx))

	// Quick search to log a representative match count
	cs, err := CompileSearch("function", &SearchOptions{LiteralSearch: true, MaxResults: 10000})
	if err != nil {
		return
	}
	resp, err := idx.SearchCompiled(cs)
	if err != nil {
		return
	}
	total := 0
	for _, fm := range resp.Matches {
		total += len(fm.Matches)
	}
	b.Logf("index: 'function' literal → %d files opened, %d files matched, %d line matches",
		resp.FilesOpened, resp.FilesWithMatch, total)
}

// formatBytes returns a human-readable byte count.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
