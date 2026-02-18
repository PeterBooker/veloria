package index

import (
	"archive/zip"
	"github.com/klauspost/compress/gzip"
	"container/heap"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxDecompressSize is the maximum allowed size for a single decompressed file (100 MB).
const maxDecompressSize = 100 << 20

// FileStat holds information about a single file.
type FileStat struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ExtractStats holds statistics collected during file extraction.
type ExtractStats struct {
	FileCount    int         `json:"file_count"`
	TotalSize    int64       `json:"total_size"`
	LargestFiles []*FileStat `json:"largest_files"`
}

// minHeap implements a min-heap for tracking largest files.
type minHeap []*FileStat

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *minHeap) Push(x any) {
	*h = append(*h, x.(*FileStat))
}

func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// Unzip extracts all files in the zip archive src into dest.
func Unzip(src, dest string) error {
	return unzipWithFilter(src, dest, nil)
}

// UnzipTextFiles extracts only "text" files (by extension) from src into dest.
// You can tweak the extensions list as needed for your trigram indexing.
func UnzipTextFiles(src, dest string) error {
	_, err := UnzipTextFilesWithStats(src, dest)
	return err
}

// textExtensions returns the set of file extensions considered as text files.
func textExtensions() map[string]bool {
	return map[string]bool{
		".txt":  true,
		".md":   true,
		".html": true,
		".xml":  true,
		".json": true,
		".yaml": true,
		".yml":  true,
		".php":  true,
		".js":   true,
		".jsx":  true,
		".css":  true,
		".ts":   true,
		".tsx":  true,
	}
}

// UnzipTextFilesWithStats extracts text files and returns extraction statistics
// including file count, total size, and the top 100 largest files.
func UnzipTextFilesWithStats(src, dest string) (*ExtractStats, error) {
	textExts := textExtensions()

	filter := func(f *zip.File) bool {
		if f.FileInfo().IsDir() {
			return true
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		return textExts[ext]
	}

	return unzipWithFilterAndStats(src, dest, filter, 100)
}

// unzipWithFilterAndStats extracts files and collects statistics.
func unzipWithFilterAndStats(src, dest string, filter func(*zip.File) bool, topN int) (stats *ExtractStats, err error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := zr.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	stripPrefix := findCommonPrefix(zr.File)
	cleanDest := filepath.Clean(dest) + string(os.PathSeparator)

	stats = &ExtractStats{}
	h := &minHeap{}
	heap.Init(h)

	for _, f := range zr.File {
		if filter != nil && !filter(f) {
			continue
		}

		name := f.Name
		if stripPrefix != "" {
			name = strings.TrimPrefix(name, stripPrefix)
			if name == "" {
				continue
			}
		}

		fpath := filepath.Join(dest, name) // #nosec G305 -- validated by cleanPath check below
		cleanPath := filepath.Clean(fpath)
		if !strings.HasPrefix(cleanPath, cleanDest) && cleanPath != filepath.Clean(dest) {
			return nil, fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, f.Mode()); err != nil {
				return nil, err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0o750); err != nil {
			return nil, err
		}

		written, err := extractFileWithSize(f, fpath)
		if err != nil {
			return nil, err
		}

		// Collect stats
		stats.FileCount++
		stats.TotalSize += written

		// Track top N largest files using min-heap
		fs := &FileStat{Path: name, Size: written}
		if h.Len() < topN {
			heap.Push(h, fs)
		} else if written > (*h)[0].Size {
			heap.Pop(h)
			heap.Push(h, fs)
		}
	}

	// Extract largest files from heap and sort descending
	stats.LargestFiles = make([]*FileStat, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		stats.LargestFiles[i] = heap.Pop(h).(*FileStat)
	}
	sort.Slice(stats.LargestFiles, func(i, j int) bool {
		return stats.LargestFiles[i].Size > stats.LargestFiles[j].Size
	})

	return stats, nil
}

// extractFileWithSize extracts a file and returns the number of bytes written.
func extractFileWithSize(f *zip.File, fpath string) (written int64, err error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := rc.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode()) // #nosec G304 -- path validated by caller
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := outFile.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	written, err = io.Copy(outFile, io.LimitReader(rc, maxDecompressSize+1))
	if err != nil {
		return 0, err
	}
	if written > maxDecompressSize {
		return 0, fmt.Errorf("file exceeds maximum decompressed size of %d bytes", maxDecompressSize)
	}
	return written, nil
}

// UnzipAllFiles extracts all files from src into dest.
func UnzipAllFiles(src, dest string) error {
	return unzipWithFilter(src, dest, nil)
}

// internal unzipping function; if filter==nil, extracts everything.
func unzipWithFilter(src, dest string, filter func(*zip.File) bool) (err error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := zr.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	// Detect common top-level directory to strip (e.g., "bbpress/" in WordPress zips)
	stripPrefix := findCommonPrefix(zr.File)

	// Clean the destination path for security comparison
	cleanDest := filepath.Clean(dest) + string(os.PathSeparator)

	for _, f := range zr.File {
		if filter != nil && !filter(f) {
			continue
		}

		// Strip common prefix from file name
		name := f.Name
		if stripPrefix != "" {
			name = strings.TrimPrefix(name, stripPrefix)
			if name == "" {
				// Skip the top-level directory itself
				continue
			}
		}

		// Security: Prevent Zip Slip attack by validating the path
		fpath := filepath.Join(dest, name) // #nosec G305 -- validated by cleanPath check below
		cleanPath := filepath.Clean(fpath)
		if !strings.HasPrefix(cleanPath, cleanDest) && cleanPath != filepath.Clean(dest) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0o750); err != nil {
			return err
		}

		// Extract file in a closure to ensure proper resource cleanup
		if err := extractFile(f, fpath); err != nil {
			return err
		}
	}

	return nil
}

// findCommonPrefix detects if all files in the zip share a common top-level directory.
// Returns the prefix to strip (e.g., "bbpress/") or empty string if no common prefix.
func findCommonPrefix(files []*zip.File) string {
	if len(files) == 0 {
		return ""
	}

	// Find the first path component of each file
	var commonDir string
	for _, f := range files {
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 {
			// File at root level with no directory - no common prefix
			return ""
		}
		dir := parts[0]
		if commonDir == "" {
			commonDir = dir
		} else if dir != commonDir {
			// Different top-level directories - no common prefix
			return ""
		}
	}

	if commonDir == "" {
		return ""
	}
	return commonDir + "/"
}

// extractFile extracts a single file from the zip archive.
// Using a separate function ensures defer calls execute after each file.
func extractFile(f *zip.File, fpath string) (err error) {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rc.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode()) // #nosec G304 -- path validated by caller
	if err != nil {
		return err
	}
	defer func() {
		if cerr := outFile.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	written, err := io.Copy(outFile, io.LimitReader(rc, maxDecompressSize+1))
	if err != nil {
		return err
	}
	if written > maxDecompressSize {
		return fmt.Errorf("file exceeds maximum decompressed size of %d bytes", maxDecompressSize)
	}
	return nil
}

// CompressSourceDir gzip-compresses every regular file in dir in place.
// Each file is written to a temp file first, then atomically renamed to avoid
// data loss on failure. This is intended to be called after the trigram index
// has been built from the uncompressed source files.
func CompressSourceDir(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return compressFileInPlace(path)
	})
}

func compressFileInPlace(path string) (err error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path from internal directory walk
	if err != nil {
		return err
	}

	tmp := path + ".gz.tmp"
	f, err := os.Create(tmp) // #nosec G304 -- path from internal directory walk
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()

	gz := gzip.NewWriter(f)
	if _, err = gz.Write(data); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}
