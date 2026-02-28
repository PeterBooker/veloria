package repo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"veloria/internal/cache"
	"veloria/internal/config"
	"veloria/internal/index"
)

const (
	UpdateInterval   = 5 * time.Minute
	IndexTimeout     = 30 * time.Minute
	FullScanInterval = 7 * 24 * time.Hour
	globalMatchCap   = 100_000
)

// ExtensionType identifies the type of extension store.
type ExtensionType string

const (
	TypePlugins ExtensionType = "plugins"
	TypeThemes  ExtensionType = "themes"
	TypeCores   ExtensionType = "cores"
)

// ExtensionStore is a generic in-memory store for managing WordPress extensions.
// It provides common functionality for loading, indexing, and searching extensions.
type ExtensionStore[T Indexable] struct {
	l        *zap.Logger
	c        *config.Config
	db       *gorm.DB
	ctx      context.Context
	cache    cache.Cache
	api      *APIClient
	repoType ExtensionType
	filesDir string

	// List holds all loaded extensions, keyed by slug
	List  map[string]T
	Total int

	mu sync.RWMutex
}

// IndexTask represents a single indexing operation ready to execute.
// The Run function contains all the work: invoking veloria-indexer,
// opening the resulting index, and swapping it into the repository.
type IndexTask struct {
	ExtensionType ExtensionType
	Slug     string
	Run      func()
}

// StoreConfig holds configuration for creating a new extension store.
type StoreConfig[T Indexable] struct {
	Ctx           context.Context
	DB            *gorm.DB
	Config        *config.Config
	Logger        *zap.Logger
	Cache         cache.Cache
	API           *APIClient
	ExtensionType ExtensionType
}

// NewExtensionStore creates a new generic extension store.
func NewExtensionStore[T Indexable](cfg StoreConfig[T]) *ExtensionStore[T] {
	return &ExtensionStore[T]{
		l:        cfg.Logger,
		c:        cfg.Config,
		db:       cfg.DB,
		ctx:      cfg.Ctx,
		cache:    cfg.Cache,
		api:      cfg.API,
		repoType: cfg.ExtensionType,
		filesDir: filepath.Join(cfg.Config.DataDir, string(cfg.ExtensionType)),
		List:     make(map[string]T),
	}
}

// LoadFromDB loads extensions from the database using the provided loader function.
func (r *ExtensionStore[T]) LoadFromDB(loader func(db *gorm.DB) ([]T, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	extensions, err := loader(r.db)
	if err != nil {
		return err
	}

	for _, ext := range extensions {
		r.List[ext.GetSlug()] = ext
	}

	r.Total = len(extensions)
	r.l.Debug("Loaded extensions", zap.Int("count", r.Total), zap.String("type", string(r.repoType)))

	return nil
}

// LoadIndexes loads all existing indexes from disk.
// It handles both the new single-directory layout (slug/) and the legacy
// versioned layout (slug.timestamp/) for backwards compatibility.
func (r *ExtensionStore[T]) LoadIndexes() error {
	indexDir := filepath.Join(r.filesDir, "index")

	dirs, err := os.ReadDir(indexDir)
	if err != nil {
		r.l.Debug("No index directory found", zap.String("type", string(r.repoType)), zap.Error(err))
		return nil
	}

	r.l.Debug("Found existing index directories", zap.Int("count", len(dirs)), zap.String("type", string(r.repoType)))

	// Group directories by slug to handle legacy versioned directories
	type indexInfo struct {
		path      string
		timestamp int64
	}
	slugToIndexes := make(map[string][]indexInfo)

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}

		name := dir.Name()
		path := filepath.Join(indexDir, name)

		// Parse legacy versioned directory names (slug.timestamp) only when the
		// suffix looks like a real Unix timestamp (prevents core versions like 6.7.1).
		slug := name
		var timestamp int64
		if idx := strings.LastIndex(name, "."); idx > 0 {
			suffix := name[idx+1:]
			if isTimestampSuffix(suffix) {
				if ts, err := strconv.ParseInt(suffix, 10, 64); err == nil {
					slug = name[:idx]
					timestamp = ts
				}
			}
		}

		slugToIndexes[slug] = append(slugToIndexes[slug], indexInfo{
			path:      path,
			timestamp: timestamp,
		})
	}

	var loaded int

	for slug, indexes := range slugToIndexes {
		// Sort by timestamp descending to try latest first
		for i := 0; i < len(indexes)-1; i++ {
			for j := i + 1; j < len(indexes); j++ {
				if indexes[j].timestamp > indexes[i].timestamp {
					indexes[i], indexes[j] = indexes[j], indexes[i]
				}
			}
		}

		// Try to load an index (prefer latest version)
		var loadedIdx *index.Index
		var loadedPath string
		for _, info := range indexes {
			idx := index.Open(info.path)
			if idx != nil {
				loadedIdx = idx
				loadedPath = info.path
				break
			}
			r.l.Warn("Failed to open index, removing", zap.String("path", info.path))
			if err := os.RemoveAll(info.path); err != nil {
				r.l.Warn("Failed to remove index", zap.String("path", info.path), zap.Error(err))
			}
		}

		if loadedIdx == nil {
			continue
		}

		// Clean up legacy versioned directories (keep only the non-versioned one)
		canonicalPath := filepath.Join(indexDir, slug)
		for _, info := range indexes {
			if info.path != loadedPath && info.path != canonicalPath {
				r.l.Debug("Removing legacy versioned index", zap.String("path", info.path))
				if err := os.RemoveAll(info.path); err != nil {
					r.l.Warn("Failed to remove legacy index", zap.String("path", info.path), zap.Error(err))
				}
			}
		}

		// Update the extension's index
		if err := r.UpdateIndex(loadedIdx, slug); err != nil {
			r.l.Debug("Skipping index", zap.String("slug", slug), zap.Error(err))
			continue
		}

		loaded++
	}

	r.l.Debug("Loaded indexes", zap.Int("loaded", loaded), zap.Int("total", len(slugToIndexes)), zap.String("type", string(r.repoType)))
	return nil
}

func isTimestampSuffix(s string) bool {
	// Unix timestamps are at least 9 digits since 2001; avoid treating versions like 6.7.1.
	if len(s) < 9 {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// Exists checks if an extension exists in the repository.
func (r *ExtensionStore[T]) Exists(slug string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.List[slug]
	return ok
}

// Get retrieves an extension by slug.
func (r *ExtensionStore[T]) Get(slug string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ext, ok := r.List[slug]
	return ext, ok
}

// Set adds or updates an extension in the repository.
func (r *ExtensionStore[T]) Set(slug string, ext T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.List[slug] = ext
	r.Total = len(r.List)
}

// Unlist removes an extension from the in-memory list but keeps the DB row.
func (r *ExtensionStore[T]) Unlist(slug string) {
	r.mu.Lock()
	delete(r.List, slug)
	r.Total = len(r.List)
	r.mu.Unlock()
}

// Remove deletes an extension from the in-memory list and the database.
func (r *ExtensionStore[T]) Remove(slug string) {
	r.Unlist(slug)

	col := "slug"
	if r.repoType == TypeCores {
		col = "version"
	}
	var zero T
	r.db.Where(col+" = ?", slug).Delete(&zero)
}

// Stats returns totals for the repository and how many extensions have indexes loaded.
func (r *ExtensionStore[T]) Stats() (total int, indexed int) {
	cacheKey := "stats:" + string(r.repoType)
	if r.cache != nil {
		if v, ok := r.cache.Get(cacheKey); ok {
			pair := v.([2]int)
			return pair[0], pair[1]
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	total = r.Total
	for _, ext := range r.List {
		if ie := ext.GetIndexedExtension(); ie != nil && ie.HasIndex() {
			indexed++
		}
	}

	if r.cache != nil {
		r.cache.Set(cacheKey, [2]int{total, indexed}, 30*time.Second)
	}

	return total, indexed
}

// IndexStatus returns a snapshot of index availability by slug.
func (r *ExtensionStore[T]) IndexStatus() map[string]bool {
	cacheKey := "index_status:" + string(r.repoType)
	if r.cache != nil {
		if v, ok := r.cache.Get(cacheKey); ok {
			return v.(map[string]bool)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	status := make(map[string]bool, len(r.List))
	for slug, ext := range r.List {
		ie := ext.GetIndexedExtension()
		status[slug] = ie != nil && ie.HasIndex()
	}

	if r.cache != nil {
		r.cache.Set(cacheKey, status, 30*time.Second)
	}

	return status
}

// UpdateIndex updates the index for a specific extension.
func (r *ExtensionStore[T]) UpdateIndex(idx *index.Index, slug string) error {
	r.mu.RLock()
	ext, ok := r.List[slug]
	r.mu.RUnlock()

	if !ok {
		return ErrExtNotFound
	}

	ext.GetIndexedExtension().UpdateIndex(idx)

	// Invalidate cached stats and index status so the next request sees the change.
	if r.cache != nil {
		r.cache.Delete("stats:" + string(r.repoType))
		r.cache.Delete("index_status:" + string(r.repoType))
	}

	return nil
}

// Search searches all extensions for the given term.
// It compiles regex once, snapshots the extension list to minimize lock scope,
// and searches extensions concurrently with a worker pool.
// If progressFn is non-nil, it is called after each extension is searched
// with the number of extensions searched so far and the total count.
func (r *ExtensionStore[T]) Search(term string, opt *index.SearchOptions, progressFn func(searched, total int)) ([]*SearchResult, error) {
	// Compile search patterns once for reuse across all extensions
	cs, err := index.CompileSearch(term, opt)
	if err != nil {
		return nil, err
	}

	// Snapshot the extension list and release the lock quickly
	r.mu.RLock()
	extensions := make([]T, 0, len(r.List))
	for _, ext := range r.List {
		if ie := ext.GetIndexedExtension(); ie != nil && ie.HasIndex() {
			extensions = append(extensions, ext)
		}
	}
	r.mu.RUnlock()

	if len(extensions) == 0 {
		return nil, nil
	}

	// Parallel search with worker pool
	workers := min(r.c.SearchConcurrency, len(extensions))

	var (
		mu           sync.Mutex
		results      []*SearchResult
		totalMatches atomic.Int64
		searched     atomic.Int64
	)

	total := len(extensions)

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, ext := range extensions {
		if totalMatches.Load() >= globalMatchCap {
			break
		}

		sem <- struct{}{}

		if totalMatches.Load() >= globalMatchCap {
			<-sem
			break
		}

		wg.Add(1)
		go func(e T) {
			defer wg.Done()
			defer func() {
				<-sem
				n := int(searched.Add(1))
				if progressFn != nil {
					progressFn(n, total)
				}
			}()

			if totalMatches.Load() >= globalMatchCap {
				return
			}

			ie := e.GetIndexedExtension()
			if ie == nil {
				return
			}

			resp, err := ie.SearchCompiled(cs)
			if err != nil {
				r.l.Warn("Failed to search extension", zap.String("slug", e.GetSlug()), zap.Error(err))
				return
			}

			if len(resp.Matches) > 0 {
				matchCount := int64(0)
				for _, fm := range resp.Matches {
					matchCount += int64(len(fm.Matches))
				}
				totalMatches.Add(matchCount)

				mu.Lock()
				results = append(results, &SearchResult{
					Extension: e,
					Matches:   resp.Matches,
				})
				mu.Unlock()
			}
		}(ext)
	}

	wg.Wait()

	return results, nil
}

// PrepareUpdates fetches pending items, saves them to the database, and returns
// IndexTasks ready for execution by a shared worker pool. The caller is
// responsible for running the tasks with appropriate concurrency control.
func (r *ExtensionStore[T]) PrepareUpdates(fetchFn func() ([]T, error), saveFn func(db *gorm.DB, ext T) error) []IndexTask {
	extensions, err := fetchFn()
	if err != nil {
		r.l.Error("Failed to fetch updates", zap.String("type", string(r.repoType)), zap.Error(err))
		return nil
	}

	if len(extensions) == 0 {
		return nil
	}

	r.l.Info("Preparing extensions for indexing", zap.Int("count", len(extensions)), zap.String("type", string(r.repoType)))

	var tasks []IndexTask

	for _, ext := range extensions {
		slug := ext.GetSlug()
		if slug == "" {
			r.l.Warn("Skipping extension with empty slug", zap.String("type", string(r.repoType)))
			continue
		}

		downloadLink := ext.GetDownloadLink()
		if downloadLink == "" {
			r.l.Warn("Skipping extension with empty download link", zap.String("type", string(r.repoType)), zap.String("slug", slug))
			continue
		}

		// Save to database (do this sequentially to avoid DB contention)
		if err := saveFn(r.db, ext); err != nil {
			r.l.Error("Failed to save extension to DB", zap.String("type", string(r.repoType)), zap.String("slug", slug), zap.Error(err))
			continue
		}

		// Reuse existing IndexedExtension when present to preserve locks/index swap.
		if existing, ok := r.Get(slug); ok {
			if existingIE := existing.GetIndexedExtension(); existingIE != nil {
				ext.SetIndexedExtension(existingIE)
			}
		}
		if ext.GetIndexedExtension() == nil {
			ext.SetIndexedExtension(NewIndexedExtension())
		}

		// Add extension to list BEFORE creating task to avoid deadlock
		r.Set(slug, ext)

		// Capture loop variables for closure
		taskExt := ext
		taskSlug := slug
		taskSource := ext.GetSource()

		tasks = append(tasks, r.makeIndexTask(taskExt, taskSlug, taskSource))
	}

	return tasks
}

// MakeReindexTask creates an IndexTask for re-indexing an already-loaded extension.
// This is used for admin-triggered ad-hoc re-indexing.
func (r *ExtensionStore[T]) MakeReindexTask(ext T) IndexTask {
	return r.makeIndexTask(ext, ext.GetSlug(), ext.GetSource())
}

// makeIndexTask creates an IndexTask that runs the indexer for the given extension.
func (r *ExtensionStore[T]) makeIndexTask(taskExt T, taskSlug, taskSource string) IndexTask {
	return IndexTask{
		ExtensionType: r.repoType,
		Slug:     taskSlug,
		Run: func() {
			r.l.Info("Indexing extension", zap.String("type", string(r.repoType)), zap.String("slug", taskSlug))

			taskIE := taskExt.GetIndexedExtension()
			taskIE.LockUpdates()
			defer taskIE.UnlockUpdates()

			result, err := r.runIndexer(taskSlug, taskExt.GetDownloadLink())
			if err != nil {
				if errors.Is(err, ErrDownloadNotFound) {
					if taskIE.HasIndex() {
						r.l.Warn("Download not found, keeping existing index", zap.String("type", string(r.repoType)), zap.String("slug", taskSlug))
					} else {
						r.l.Warn("Download not found, skipping", zap.String("type", string(r.repoType)), zap.String("slug", taskSlug))
						r.Unlist(taskSlug)
					}
					return
				}
				r.l.Error("Indexer failed", zap.String("slug", taskSlug), zap.Error(err))
				return
			}

			if result.Stats != nil {
				r.saveExtractStats(taskSlug, taskSource, taskExt.GetName(), result.Stats)
			}

			newIdx := index.Open(result.IndexPath)
			if newIdx == nil {
				r.l.Error("Failed to open new index", zap.String("path", result.IndexPath))
				return
			}

			if err := r.UpdateIndex(newIdx, taskSlug); err != nil {
				r.l.Error("Failed to update index", zap.String("slug", taskSlug), zap.Error(err))
				return
			}

			r.l.Info("Successfully indexed", zap.String("slug", taskSlug))
		},
	}
}

// IndexerResult contains the output from running the indexer.
type IndexerResult struct {
	IndexPath string
	Stats     *ExtractStats
}

// runIndexer executes the veloria-indexer command and returns the index path and stats.
func (r *ExtensionStore[T]) runIndexer(slug, downloadLink string) (*IndexerResult, error) {
	ctx, cancel := context.WithTimeout(r.ctx, IndexTimeout)
	defer cancel()

	cmd := exec.CommandContext( // #nosec G204 -- args are from internal DB, not user input
		ctx,
		"veloria-indexer",
		"-repo="+string(r.repoType),
		"-slug="+slug,
		"-zipurl="+downloadLink,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if strings.Contains(stderrStr, "404") {
			return nil, ErrDownloadNotFound
		}
		return nil, fmt.Errorf("%w: %s", err, stderrStr)
	}

	// Parse INDEX_READY:<path> and EXTRACT_STATS:<json> from stdout
	result := &IndexerResult{}
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "INDEX_READY:"); ok {
			result.IndexPath = after
		} else if after, ok := strings.CutPrefix(line, "EXTRACT_STATS:"); ok {
			statsJSON := after
			var stats ExtractStats
			if err := json.Unmarshal([]byte(statsJSON), &stats); err == nil {
				result.Stats = &stats
			}
		}
	}

	if result.IndexPath == "" {
		return nil, ErrIndexNotReady
	}

	return result, nil
}

// saveExtractStats saves extraction statistics to the database for the given extension.
func (r *ExtensionStore[T]) saveExtractStats(slug, source, name string, stats *ExtractStats) {
	tableName := string(r.repoType)
	identifierCol := "slug"
	if r.repoType == TypeCores {
		identifierCol = "version"
	}

	query := r.db.Table(tableName).Where(identifierCol+" = ?", slug)
	// Plugins and themes have a source column; cores do not.
	if r.repoType != TypeCores {
		query = query.Where("source = ?", source)
	}

	err := query.Updates(map[string]any{
		"file_count":    stats.FileCount,
		"total_size":    stats.TotalSize,
		"largest_files": stats.LargestFiles,
	}).Error

	if err != nil {
		r.l.Error("Failed to save extract stats", zap.String("slug", slug), zap.Error(err))
	}

	r.saveLargestRepoFiles(slug, name, stats.LargestFiles)
}

// saveLargestRepoFiles replaces the largest_repo_files rows for a given extension.
func (r *ExtensionStore[T]) saveLargestRepoFiles(slug, name string, files []*FileStat) {
	repoType := string(r.repoType)

	// Remove old entries for this extension.
	if err := r.db.Table("largest_repo_files").
		Where("repo_type = ? AND slug = ?", repoType, slug).
		Delete(nil).Error; err != nil {
		r.l.Error("Failed to delete old largest_repo_files", zap.String("slug", slug), zap.Error(err))
		return
	}

	if len(files) == 0 {
		return
	}

	// Batch insert new entries.
	rows := make([]map[string]any, len(files))
	for i, f := range files {
		rows[i] = map[string]any{
			"repo_type": repoType,
			"slug":      slug,
			"name":      name,
			"path":      f.Path,
			"size":      f.Size,
		}
	}

	if err := r.db.Table("largest_repo_files").Create(rows).Error; err != nil {
		r.l.Error("Failed to insert largest_repo_files", zap.String("slug", slug), zap.Error(err))
	}
}

// ResumeUnindexed returns IndexTasks for extensions that are in memory (loaded
// from DB) but don't have an index on disk. This handles the case where the
// server was restarted mid-indexing.
func (r *ExtensionStore[T]) ResumeUnindexed() []IndexTask {
	r.mu.RLock()
	var unindexed []T
	for _, ext := range r.List {
		ie := ext.GetIndexedExtension()
		if (ie == nil || !ie.HasIndex()) && ext.GetDownloadLink() != "" {
			unindexed = append(unindexed, ext)
		}
	}
	r.mu.RUnlock()

	if len(unindexed) == 0 {
		return nil
	}

	r.l.Info("Resuming unindexed extensions", zap.Int("count", len(unindexed)), zap.String("type", string(r.repoType)))

	var tasks []IndexTask
	for _, ext := range unindexed {
		taskExt := ext
		taskSlug := ext.GetSlug()
		taskSource := ext.GetSource()

		tasks = append(tasks, r.makeIndexTask(taskExt, taskSlug, taskSource))
	}

	return tasks
}

// GetAll returns all extensions as a slice.
func (r *ExtensionStore[T]) GetAll() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]T, 0, len(r.List))
	for _, ext := range r.List {
		result = append(result, ext)
	}
	return result
}

// Type returns the extension type.
func (r *ExtensionStore[T]) Type() ExtensionType {
	return r.repoType
}

// GetExtension retrieves an extension by slug, returning it as the Extension interface.
func (r *ExtensionStore[T]) GetExtension(slug string) (Extension, bool) {
	ext, ok := r.Get(slug)
	if !ok {
		var zero T
		return zero, false
	}
	return ext, true
}

// MakeReindexTaskBySlug creates an IndexTask for re-indexing an extension by slug.
// Returns false if the extension is not found.
func (r *ExtensionStore[T]) MakeReindexTaskBySlug(slug string) (IndexTask, bool) {
	ext, ok := r.Get(slug)
	if !ok {
		return IndexTask{}, false
	}
	return r.MakeReindexTask(ext), true
}

// ResolveSourceDir returns the parent directory of the extension's source files.
func (r *ExtensionStore[T]) ResolveSourceDir(slug string) (string, error) {
	ext, ok := r.Get(slug)
	if !ok {
		return "", ErrExtNotFound
	}
	ie := ext.GetIndexedExtension()
	if ie == nil {
		return "", ErrNoIndex
	}
	idx := ie.GetIndex()
	if idx == nil {
		return "", ErrNoIndex
	}
	return filepath.Dir(idx.SourceDir()), nil
}
