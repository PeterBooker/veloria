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
)

// Extension defines the common interface for all WordPress extension types.
type Extension interface {
	GetSlug() string
	GetSource() string
	GetName() string
	GetVersion() string
	GetDownloadLink() string
	GetActiveInstalls() int

	// IndexedExtension wiring
	GetIndexedExtension() *IndexedExtension
	SetIndexedExtension(ext *IndexedExtension)

	// Index management
	GetIndex() *index.Index
	SetIndex(idx *index.Index)
	HasIndex() bool
	Search(term string, opt *index.SearchOptions) (*index.SearchResponse, error)
	UpdateIndex(idx *index.Index)

	// Update locking
	LockUpdates()
	UnlockUpdates()
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
func (ie *IndexedExtension) Search(term string, opt *index.SearchOptions) (*index.SearchResponse, error) {
	ie.updateMu.RLock()
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
func (ie *IndexedExtension) SearchCompiled(cs *index.CompiledSearch) (*index.SearchResponse, error) {
	ie.updateMu.RLock()
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

	// Close old index to release mmap after a delay
	if oldIdx != nil {
		go closeOldIndex(oldIdx)
	}
}

// closeOldIndex closes an old index after a delay.
// The delay ensures no readers are still accessing the mmap'd files.
func closeOldIndex(idx *index.Index) {
	time.Sleep(5 * time.Second)
	idx.Close()
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
	FileCount    int                            `json:"file_count"`
	TotalSize    int64                          `json:"total_size"`
	LargestFiles datatypes.JSONSlice[*FileStat] `json:"largest_files"`
}
