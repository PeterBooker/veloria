package repo

import (
	"errors"
	"sync"
	"time"

	"gorm.io/datatypes"

	"veloria/internal/index"
)

// Common errors
var (
	ErrNoIndex          = errors.New("extension has no index")
	ErrExtNotFound      = errors.New("extension not found")
	ErrEmptySlug        = errors.New("extension has empty slug")
	ErrIndexNotReady    = errors.New("index not ready")
	ErrDownloadNotFound = errors.New("download not found")
	ErrDownloadSkipped  = errors.New("download not found, extension skipped")
)

// Extension defines the data contract for all WordPress extension types.
// This is the narrow interface used by API handlers, templates, and search results.
type Extension interface {
	GetSlug() string
	GetSource() string
	GetName() string
	GetVersion() string
	GetDownloadLink() string
	GetActiveInstalls() int
	GetDownloaded() int
}

// Indexable extends Extension with index wiring methods.
// This is the constraint for the generic ExtensionStore[T] and is used
// internally by the store and manager — not by API handlers or templates.
type Indexable interface {
	Extension
	GetIndexedExtension() *IndexedExtension
	SetIndexedExtension(ext *IndexedExtension)
}

// IndexedExtension provides index management for extensions.
// Embed as a pointer (*IndexedExtension) in extension types to avoid copying the mutex.
type IndexedExtension struct {
	idx *index.Index
	mu  sync.RWMutex

	updateMu sync.RWMutex
}

// NewIndexedExtension creates a new IndexedExtension.
func NewIndexedExtension() *IndexedExtension {
	return &IndexedExtension{}
}

func (ie *IndexedExtension) GetIndex() *index.Index {
	ie.mu.RLock()
	defer ie.mu.RUnlock()
	return ie.idx
}

func (ie *IndexedExtension) SetIndex(idx *index.Index) {
	ie.mu.Lock()
	defer ie.mu.Unlock()
	ie.idx = idx
}

func (ie *IndexedExtension) HasIndex() bool {
	ie.mu.RLock()
	defer ie.mu.RUnlock()
	return ie.idx != nil
}

func (ie *IndexedExtension) LockUpdates() {
	ie.updateMu.Lock()
}

func (ie *IndexedExtension) UnlockUpdates() {
	ie.updateMu.Unlock()
}

// Search searches the extension's index for the given term.
// If the extension is currently being re-indexed, it returns nil to avoid
// blocking the search on a long-running indexer subprocess.
func (ie *IndexedExtension) Search(term string, opt *index.SearchOptions) (*index.SearchResponse, error) {
	if !ie.updateMu.TryRLock() {
		return nil, nil // extension is being re-indexed, skip
	}
	defer ie.updateMu.RUnlock()

	ie.mu.RLock()
	idx := ie.idx
	ie.mu.RUnlock()

	if idx == nil {
		return nil, ErrNoIndex
	}

	return idx.Search(term, opt)
}

// SearchCompiled searches the extension's index using pre-compiled patterns.
// Uses TryRLock to avoid blocking on extensions being re-indexed — if the
// update lock is held, the extension is skipped rather than stalling the
// entire search worker pool.
func (ie *IndexedExtension) SearchCompiled(cs *index.CompiledSearch) (*index.SearchResponse, error) {
	if !ie.updateMu.TryRLock() {
		return nil, nil // extension is being re-indexed, skip
	}
	defer ie.updateMu.RUnlock()

	ie.mu.RLock()
	idx := ie.idx
	ie.mu.RUnlock()

	if idx == nil {
		return nil, ErrNoIndex
	}

	return idx.SearchCompiled(cs)
}

// UpdateIndex safely swaps the current index with a new one.
// The old index is closed asynchronously after a delay to ensure no readers
// are still accessing the mmap'd files.
func (ie *IndexedExtension) UpdateIndex(idx *index.Index) {
	ie.mu.Lock()
	oldIdx := ie.idx
	ie.idx = idx
	ie.mu.Unlock()

	if oldIdx != nil {
		scheduleIndexClose(oldIdx)
	}
}

// indexCloseQueue is a bounded channel that limits the number of concurrent
// old-index cleanup goroutines. A dedicated goroutine drains the queue,
// preventing unbounded goroutine creation during bulk reindex operations.
var indexCloseQueue = make(chan *index.Index, 64)

func init() {
	go func() {
		for idx := range indexCloseQueue {
			time.Sleep(5 * time.Second)
			idx.Close()
		}
	}()
}

// scheduleIndexClose queues an old index for deferred closing. If the queue
// is full, it falls back to closing in a single bounded goroutine.
func scheduleIndexClose(idx *index.Index) {
	select {
	case indexCloseQueue <- idx:
	default:
		// Queue full — close synchronously in one goroutine to stay bounded.
		go func() {
			time.Sleep(5 * time.Second)
			idx.Close()
		}()
	}
}

// SearchResult contains search results for a single extension.
type SearchResult struct {
	Extension Extension
	Matches   []*index.FileMatch
}

// FileStat holds information about a single file from extraction stats.
type FileStat struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ExtractStats holds statistics collected during file extraction.
type ExtractStats struct {
	FileCount     int                            `json:"file_count"`
	TextFileCount int                            `json:"text_file_count"`
	TotalSize     int64                          `json:"total_size"`
	LargestFiles  datatypes.JSONSlice[*FileStat] `json:"largest_files"`
}
