package index

import (
	"archive/zip"
	"container/heap"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/gzip"

	cindex "veloria/internal/codesearch/index"
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
	FileCount     int         `json:"file_count"`
	TextFileCount int         `json:"text_file_count"`
	TotalSize     int64       `json:"total_size"`
	LargestFiles  []*FileStat `json:"largest_files"`
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

// indexableExtensions returns the set of file extensions that should be included
// in the trigram search index. Used during index building (not extraction) to
// filter which files get trigram-indexed.
func indexableExtensions() map[string]bool {
	return map[string]bool{
		".txt":      true,
		".md":       true,
		".html":     true,
		".xml":      true,
		".json":     true,
		".yaml":     true,
		".yml":      true,
		".php":      true,
		".js":       true,
		".jsx":      true,
		".css":      true,
		".ts":       true,
		".tsx":      true,
		".sql":      true,
		".pot":      true,
		".twig":     true,
		".mustache": true,
		".svg":      true,
		".less":     true,
		".scss":     true,
		".sass":     true,
		".vue":      true,
		".svelte":   true,
	}
}

// UnzipWithStats extracts all files from the ZIP and returns extraction statistics
// including file count, total size, and the top 100 largest files.
// TextFileCount tracks files matching indexableExtensions() for index coverage reporting.
func UnzipWithStats(src, dest string) (*ExtractStats, error) {
	return unzipWithFilterAndStats(src, dest, nil, 100)
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
	textExts := indexableExtensions()
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
		if textExts[strings.ToLower(filepath.Ext(name))] {
			stats.TextFileCount++
		}

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

// flushQuiet calls ix.Flush() with the standard logger silenced,
// suppressing the codesearch library's unconditional log output.
func flushQuiet(ix *cindex.IndexWriter) {
	prev := log.Writer()
	log.SetOutput(io.Discard)
	ix.Flush()
	log.SetOutput(prev)
}

// IndexDirToFile creates a trigram index from indexSrc and writes it to trigramsPath.
// Only files with extensions in indexableExtensions() are added to the index.
func IndexDirToFile(indexSrc string, trigramsPath string) *cindex.IndexWriter {
	// Ensure parent directory exists and "touch" the trigrams file.
	if err := os.MkdirAll(filepath.Dir(trigramsPath), 0o750); err != nil {
		log.Fatalf("failed to create index directory %q: %v", filepath.Dir(trigramsPath), err)
	}
	if _, err := os.Stat(trigramsPath); os.IsNotExist(err) {
		f, err := os.OpenFile(trigramsPath, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path built from internal config
		if err != nil {
			log.Fatalf("failed to create trigrams file %q: %v", trigramsPath, err)
		}
		_ = f.Close()
	} else if err != nil {
		log.Fatalf("failed to stat trigrams file %q: %v", trigramsPath, err)
	}

	ix := cindex.Create(trigramsPath)
	ix.Verbose = false
	ix.Zip = false

	textExts := indexableExtensions()

	var roots []cindex.Path
	roots = append(roots, cindex.MakePath(filepath.Join(indexSrc)))
	ix.AddRoots(roots)

	for _, root := range roots {
		if err := filepath.Walk(root.String(), func(path string, info os.FileInfo, err error) error {
			if _, elem := filepath.Split(path); elem != "" {
				// Skip temporary or hidden files/dirs.
				if elem[0] == '.' || elem[0] == '#' || elem[0] == '~' || elem[len(elem)-1] == '~' {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			if err != nil {
				log.Printf("%s: %s", path, err)
				return nil
			}

			if info != nil && info.Mode()&os.ModeType == 0 {
				// Only index files with known text extensions.
				ext := strings.ToLower(filepath.Ext(path))
				if !textExts[ext] {
					return nil
				}
				if err := ix.AddFile(path); err != nil {
					return nil
				}
			}
			return nil
		}); err != nil {
			log.Printf("failed to walk %s: %v", root, err)
		}
	}

	flushQuiet(ix)
	return ix
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
