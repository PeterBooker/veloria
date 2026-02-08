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
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
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

// RepoType identifies the type of repository.
type RepoType string

const (
	RepoPlugins RepoType = "plugins"
	RepoThemes  RepoType = "themes"
	RepoCores   RepoType = "cores"
)

// Repository is a generic repository for managing WordPress extensions.
// It provides common functionality for loading, indexing, and searching extensions.
type Repository[T Extension] struct {
	l        *zerolog.Logger
	c        *config.Config
	db       *gorm.DB
	ctx      context.Context
	cache    cache.Cache
	repoType RepoType
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
	RepoType RepoType
	Slug     string
	Run      func()
}

// RepositoryConfig holds configuration for creating a new repository.
type RepositoryConfig[T Extension] struct {
	Ctx      context.Context
	DB       *gorm.DB
	Config   *config.Config
	Logger   *zerolog.Logger
	Cache    cache.Cache
	RepoType RepoType
}

// NewRepository creates a new generic repository.
func NewRepository[T Extension](cfg RepositoryConfig[T]) *Repository[T] {
	return &Repository[T]{
		l:        cfg.Logger,
		c:        cfg.Config,
		db:       cfg.DB,
		ctx:      cfg.Ctx,
		cache:    cfg.Cache,
		repoType: cfg.RepoType,
		filesDir: filepath.Join(cfg.Config.DataDir, string(cfg.RepoType)),
		List:     make(map[string]T),
	}
}

// LoadFromDB loads extensions from the database using the provided loader function.
func (r *Repository[T]) LoadFromDB(loader func(db *gorm.DB) ([]T, error)) error {
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
	r.l.Debug().Msgf("Loaded %d %s", r.Total, r.repoType)

	return nil
}

// LoadIndexes loads all existing indexes from disk.
// It handles both the new single-directory layout (slug/) and the legacy
// versioned layout (slug.timestamp/) for backwards compatibility.
func (r *Repository[T]) LoadIndexes() error {
	indexDir := filepath.Join(r.filesDir, "index")

	dirs, err := os.ReadDir(indexDir)
	if err != nil {
		r.l.Debug().Msgf("No index directory found for %s: %s", r.repoType, err)
		return nil
	}

	r.l.Debug().Msgf("Found %d existing %s index directories", len(dirs), r.repoType)

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
			r.l.Warn().Msgf("Failed to open index at %s, removing", info.path)
			if err := os.RemoveAll(info.path); err != nil {
				r.l.Warn().Err(err).Msgf("Failed to remove index at %s", info.path)
			}
		}

		if loadedIdx == nil {
			continue
		}

		// Clean up legacy versioned directories (keep only the non-versioned one)
		canonicalPath := filepath.Join(indexDir, slug)
		for _, info := range indexes {
			if info.path != loadedPath && info.path != canonicalPath {
				r.l.Debug().Msgf("Removing legacy versioned index at %s", info.path)
				if err := os.RemoveAll(info.path); err != nil {
					r.l.Warn().Err(err).Msgf("Failed to remove legacy index at %s", info.path)
				}
			}
		}

		// Update the extension's index
		if err := r.UpdateIndex(loadedIdx, slug); err != nil {
			r.l.Debug().Msgf("Skipping index for %s: %s", slug, err)
			continue
		}

		loaded++
	}

	r.l.Debug().Msgf("Loaded %d/%d indexes for %s", loaded, len(slugToIndexes), r.repoType)
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
func (r *Repository[T]) Exists(slug string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.List[slug]
	return ok
}

// Get retrieves an extension by slug.
func (r *Repository[T]) Get(slug string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ext, ok := r.List[slug]
	return ext, ok
}

// Set adds or updates an extension in the repository.
func (r *Repository[T]) Set(slug string, ext T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.List[slug] = ext
	r.Total = len(r.List)
}

// Unlist removes an extension from the in-memory list but keeps the DB row.
func (r *Repository[T]) Unlist(slug string) {
	r.mu.Lock()
	delete(r.List, slug)
	r.Total = len(r.List)
	r.mu.Unlock()
}

// Remove deletes an extension from the in-memory list and the database.
func (r *Repository[T]) Remove(slug string) {
	r.Unlist(slug)

	col := "slug"
	if r.repoType == RepoCores {
		col = "version"
	}
	var zero T
	r.db.Where(col+" = ?", slug).Delete(&zero)
}

// Stats returns totals for the repository and how many extensions have indexes loaded.
func (r *Repository[T]) Stats() (total int, indexed int) {
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
		if ext.HasIndex() {
			indexed++
		}
	}

	if r.cache != nil {
		r.cache.Set(cacheKey, [2]int{total, indexed}, 30*time.Second)
	}

	return total, indexed
}

// IndexStatus returns a snapshot of index availability by slug.
func (r *Repository[T]) IndexStatus() map[string]bool {
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
		status[slug] = ext.HasIndex()
	}

	if r.cache != nil {
		r.cache.Set(cacheKey, status, 30*time.Second)
	}

	return status
}

// UpdateIndex updates the index for a specific extension.
func (r *Repository[T]) UpdateIndex(idx *index.Index, slug string) error {
	r.mu.RLock()
	ext, ok := r.List[slug]
	r.mu.RUnlock()

	if !ok {
		return ErrExtNotFound
	}

	ext.UpdateIndex(idx)

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
func (r *Repository[T]) Search(term string, opt *index.SearchOptions) ([]*SearchResult, error) {
	// Compile search patterns once for reuse across all extensions
	cs, err := index.CompileSearch(term, opt)
	if err != nil {
		return nil, err
	}

	// Snapshot the extension list and release the lock quickly
	r.mu.RLock()
	extensions := make([]T, 0, len(r.List))
	for _, ext := range r.List {
		if ext.HasIndex() {
			extensions = append(extensions, ext)
		}
	}
	r.mu.RUnlock()

	if len(extensions) == 0 {
		return nil, nil
	}

	// Parallel search with worker pool
	workers := max(runtime.NumCPU(), 4)
	if workers > len(extensions) {
		workers = len(extensions)
	}

	var (
		mu           sync.Mutex
		results      []*SearchResult
		totalMatches atomic.Int64
	)

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
			defer func() { <-sem }()

			if totalMatches.Load() >= globalMatchCap {
				return
			}

			ie := e.GetIndexedExtension()
			if ie == nil {
				return
			}

			resp, err := ie.SearchCompiled(cs)
			if err != nil {
				r.l.Warn().Err(err).Msgf("Failed to search %s", e.GetSlug())
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
func (r *Repository[T]) PrepareUpdates(fetchFn func() ([]T, error), saveFn func(db *gorm.DB, ext T) error) []IndexTask {
	extensions, err := fetchFn()
	if err != nil {
		r.l.Error().Err(err).Msgf("Failed to fetch updated %s", r.repoType)
		return nil
	}

	if len(extensions) == 0 {
		return nil
	}

	r.l.Info().Msgf("Preparing %d %s for indexing", len(extensions), r.repoType)

	var tasks []IndexTask

	for _, ext := range extensions {
		slug := ext.GetSlug()
		if slug == "" {
			r.l.Warn().Msgf("Skipping %s with empty slug", r.repoType)
			continue
		}

		downloadLink := ext.GetDownloadLink()
		if downloadLink == "" {
			r.l.Warn().Msgf("Skipping %s %s with empty download link", r.repoType, slug)
			continue
		}

		// Save to database (do this sequentially to avoid DB contention)
		if err := saveFn(r.db, ext); err != nil {
			r.l.Error().Err(err).Msgf("Failed to save %s %s to DB", r.repoType, slug)
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

// makeIndexTask creates an IndexTask that runs the indexer for the given extension.
func (r *Repository[T]) makeIndexTask(taskExt T, taskSlug, taskSource string) IndexTask {
	return IndexTask{
		RepoType: r.repoType,
		Slug:     taskSlug,
		Run: func() {
			r.l.Info().Msgf("Indexing %s: %s", r.repoType, taskSlug)

			taskExt.LockUpdates()
			defer taskExt.UnlockUpdates()

			result, err := r.runIndexer(taskSlug, taskExt.GetDownloadLink())
			if err != nil {
				if errors.Is(err, ErrDownloadNotFound) {
					if taskExt.HasIndex() {
						r.l.Warn().Msgf("Download not found for %s %s, keeping existing index", r.repoType, taskSlug)
					} else {
						r.l.Warn().Msgf("Download not found for %s %s, skipping", r.repoType, taskSlug)
						r.Unlist(taskSlug)
					}
					return
				}
				r.l.Error().Err(err).Msgf("Indexer failed for %s", taskSlug)
				return
			}

			if result.Stats != nil {
				r.saveExtractStats(taskSlug, taskSource, taskExt.GetName(), result.Stats)
			}

			newIdx := index.Open(result.IndexPath)
			if newIdx == nil {
				r.l.Error().Msgf("Failed to open new index at %s", result.IndexPath)
				return
			}

			if err := r.UpdateIndex(newIdx, taskSlug); err != nil {
				r.l.Error().Err(err).Msgf("Failed to update index for %s", taskSlug)
				return
			}

			r.l.Info().Msgf("Successfully indexed %s", taskSlug)
		},
	}
}

// IndexerResult contains the output from running the indexer.
type IndexerResult struct {
	IndexPath string
	Stats     *ExtractStats
}

// runIndexer executes the veloria-indexer command and returns the index path and stats.
func (r *Repository[T]) runIndexer(slug, downloadLink string) (*IndexerResult, error) {
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
		if strings.HasPrefix(line, "INDEX_READY:") {
			result.IndexPath = strings.TrimPrefix(line, "INDEX_READY:")
		} else if strings.HasPrefix(line, "EXTRACT_STATS:") {
			statsJSON := strings.TrimPrefix(line, "EXTRACT_STATS:")
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
func (r *Repository[T]) saveExtractStats(slug, source, name string, stats *ExtractStats) {
	tableName := string(r.repoType)
	identifierCol := "slug"
	if r.repoType == RepoCores {
		identifierCol = "version"
	}

	query := r.db.Table(tableName).Where(identifierCol+" = ?", slug)
	// Plugins and themes have a source column; cores do not.
	if r.repoType != RepoCores {
		query = query.Where("source = ?", source)
	}

	err := query.Updates(map[string]any{
		"file_count":    stats.FileCount,
		"total_size":    stats.TotalSize,
		"largest_files": stats.LargestFiles,
	}).Error

	if err != nil {
		r.l.Error().Err(err).Msgf("Failed to save extract stats for %s", slug)
	}

	r.saveLargestRepoFiles(slug, name, stats.LargestFiles)
}

// saveLargestRepoFiles replaces the largest_repo_files rows for a given extension.
func (r *Repository[T]) saveLargestRepoFiles(slug, name string, files []*FileStat) {
	repoType := string(r.repoType)

	// Remove old entries for this extension.
	if err := r.db.Table("largest_repo_files").
		Where("repo_type = ? AND slug = ?", repoType, slug).
		Delete(nil).Error; err != nil {
		r.l.Error().Err(err).Msgf("Failed to delete old largest_repo_files for %s", slug)
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
		r.l.Error().Err(err).Msgf("Failed to insert largest_repo_files for %s", slug)
	}
}

// ResumeUnindexed returns IndexTasks for extensions that are in memory (loaded
// from DB) but don't have an index on disk. This handles the case where the
// server was restarted mid-indexing.
func (r *Repository[T]) ResumeUnindexed() []IndexTask {
	r.mu.RLock()
	var unindexed []T
	for _, ext := range r.List {
		if !ext.HasIndex() && ext.GetDownloadLink() != "" {
			unindexed = append(unindexed, ext)
		}
	}
	r.mu.RUnlock()

	if len(unindexed) == 0 {
		return nil
	}

	r.l.Info().Msgf("Resuming %d unindexed %s", len(unindexed), r.repoType)

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
func (r *Repository[T]) GetAll() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]T, 0, len(r.List))
	for _, ext := range r.List {
		result = append(result, ext)
	}
	return result
}

// RepoType returns the repository type.
func (r *Repository[T]) Type() RepoType {
	return r.repoType
}
