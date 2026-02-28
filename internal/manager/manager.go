package manager

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"veloria/internal/index"
	"veloria/internal/repo"
)

type Manager struct {
	sources map[repo.ExtensionType]repo.DataSource
	adhocCh chan repo.IndexTask
}

// NewManager creates a new Manager, initializes all data sources, and starts
// a single shared updater loop that pulls work from all sources.
func NewManager(ctx context.Context, l *zap.Logger, sources []repo.DataSource, concurrency int) (*Manager, error) {
	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)

	loadSource := func(name string, loadFn func() error) {
		defer wg.Done()
		if err := loadFn(); err != nil {
			errMu.Lock()
			if firstErr == nil {
				firstErr = fmt.Errorf("%s load failed: %w", name, err)
			}
			errMu.Unlock()
		}
	}

	// Load data and indexes from disk concurrently.
	wg.Add(len(sources))
	for _, ds := range sources {
		go loadSource(string(ds.Type()), ds.Load)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	srcMap := make(map[repo.ExtensionType]repo.DataSource, len(sources))
	for _, ds := range sources {
		srcMap[ds.Type()] = ds
	}

	m := &Manager{sources: srcMap, adhocCh: make(chan repo.IndexTask, 32)}
	m.startUpdater(ctx, l, concurrency)

	return m, nil
}

// NewTestManager creates a Manager for testing without loading data or starting
// the updater goroutine. It is intentionally minimal so unit tests stay fast.
func NewTestManager(l *zap.Logger, sources []repo.DataSource) (*Manager, error) {
	srcMap := make(map[repo.ExtensionType]repo.DataSource, len(sources))
	for _, ds := range sources {
		srcMap[ds.Type()] = ds
	}
	return &Manager{sources: srcMap, adhocCh: make(chan repo.IndexTask, 32)}, nil
}

// GetSource returns the DataSource for the given extension type, or nil.
func (m *Manager) GetSource(t repo.ExtensionType) repo.DataSource {
	return m.sources[t]
}

// startUpdater runs a single background loop that collects pending work from
// all sources and executes it through a shared worker pool, giving you one
// place to control total concurrency via INDEXER_CONCURRENCY.
func (m *Manager) startUpdater(ctx context.Context, l *zap.Logger, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}

	go func() {
		sem := make(chan struct{}, concurrency)

		// Resume any extensions that were saved to DB but not indexed (e.g. interrupted runs).
		var resumeTasks []repo.IndexTask
		for _, ds := range m.sources {
			resumeTasks = append(resumeTasks, ds.ResumeUnindexed()...)
		}
		if len(resumeTasks) > 0 {
			l.Info("Resuming unindexed extensions", zap.Int("count", len(resumeTasks)))
			var wg sync.WaitGroup
			for _, task := range resumeTasks {
				wg.Add(1)
				go func(t repo.IndexTask) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					t.Run()
				}(task)
			}
			wg.Wait()
		}

		for {
			select {
			case <-ctx.Done():
				l.Info("Stopping indexer updater")
				return
			default:
			}

			// Collect tasks from all sources
			var tasks []repo.IndexTask
			for _, ds := range m.sources {
				tasks = append(tasks, ds.PrepareUpdates()...)
			}

			if len(tasks) > 0 {
				l.Info("Processing index tasks", zap.Int("count", len(tasks)), zap.Int("concurrency", concurrency))

				var wg sync.WaitGroup
				for _, task := range tasks {
					wg.Add(1)
					go func(t repo.IndexTask) {
						defer wg.Done()

						sem <- struct{}{}
						defer func() { <-sem }()

						t.Run()
					}(task)
				}
				wg.Wait()
			} else {
				l.Info("No pending updates")
			}

			// Drain any ad-hoc re-index requests queued while processing the batch.
			m.drainAdhoc(sem)

			select {
			case <-time.After(repo.UpdateInterval):
			case task := <-m.adhocCh:
				l.Info("Ad-hoc re-index request", zap.String("type", string(task.ExtensionType)), zap.String("slug", task.Slug))
				sem <- struct{}{}
				go func(t repo.IndexTask) {
					defer func() { <-sem }()
					t.Run()
				}(task)
			case <-ctx.Done():
				l.Info("Stopping indexer updater")
				return
			}
		}
	}()
}

// drainAdhoc processes any pending ad-hoc re-index tasks from the channel.
func (m *Manager) drainAdhoc(sem chan struct{}) {
	for {
		select {
		case task := <-m.adhocCh:
			sem <- struct{}{}
			go func(t repo.IndexTask) {
				defer func() { <-sem }()
				t.Run()
			}(task)
		default:
			return
		}
	}
}

// SubmitReindex queues an ad-hoc re-index task for the given extension.
// Returns false if the extension is not found or the queue is full.
func (m *Manager) SubmitReindex(repoType, slug string) bool {
	ds, ok := m.sources[repo.ExtensionType(repoType)]
	if !ok {
		return false
	}

	task, found := ds.MakeReindexTaskBySlug(slug)
	if !found {
		return false
	}

	select {
	case m.adhocCh <- task:
		return true
	default:
		return false
	}
}

// Stats returns the total and indexed counts for the given extension type.
func (m *Manager) Stats(repoType string) (total int, indexed int, ok bool) {
	ds, found := m.sources[repo.ExtensionType(repoType)]
	if !found {
		return 0, 0, false
	}
	total, indexed = ds.Stats()
	return total, indexed, true
}

// IndexStatus returns a snapshot of index availability by slug for the given type.
func (m *Manager) IndexStatus(repoType string) map[string]bool {
	ds, ok := m.sources[repo.ExtensionType(repoType)]
	if !ok {
		return nil
	}
	return ds.IndexStatus()
}

// GetExtension retrieves an extension from the specified source.
func (m *Manager) GetExtension(repoType string, slug string) (repo.Extension, bool) {
	ds, ok := m.sources[repo.ExtensionType(repoType)]
	if !ok {
		return nil, false
	}
	return ds.GetExtension(slug)
}

// ResolveSourceDir returns the source directory for an extension.
func (m *Manager) ResolveSourceDir(repoType string, slug string) (string, error) {
	ds, ok := m.sources[repo.ExtensionType(repoType)]
	if !ok {
		return "", fmt.Errorf("unknown extension type: %s", repoType)
	}
	return ds.ResolveSourceDir(slug)
}

// SearchResult represents search results from a single extension (plugin/theme/core).
type SearchResult struct {
	Slug           string             `json:"slug"`
	Name           string             `json:"name"`
	Version        string             `json:"version"`
	ActiveInstalls int                `json:"active_installs"`
	Downloaded     int                `json:"downloaded"`
	Matches        []*index.FileMatch `json:"matches"`
	TotalMatches   int                `json:"total_matches"`
}

// SearchResponse contains all search results.
type SearchResponse struct {
	Results []*SearchResult `json:"results"`
	Total   int             `json:"total"`
}

// ProgressFunc is called during search to report progress.
// searched is the number of extensions searched so far, total is the total count.
type ProgressFunc func(searched, total int)

// SearchParams contains parameters for a search query.
type SearchParams struct {
	FileMatch        string
	ExcludeFileMatch string
	CaseInsensitive  bool
	LinesOfContext   uint
	OnProgress       ProgressFunc
}

// Search searches the specified source for the given term.
func (m *Manager) Search(repoType string, term string, params *SearchParams) (*SearchResponse, error) {
	if params == nil {
		params = &SearchParams{}
	}

	ds, ok := m.sources[repo.ExtensionType(repoType)]
	if !ok {
		return nil, fmt.Errorf("unknown extension type: %s", repoType)
	}

	opt := &index.SearchOptions{
		IgnoreCase:        params.CaseInsensitive,
		LiteralSearch:     false,
		LinesOfContext:    params.LinesOfContext,
		FileRegexp:        params.FileMatch,
		ExcludeFileRegexp: params.ExcludeFileMatch,
		MaxResults:        100,
	}

	results, err := ds.Search(term, opt, params.OnProgress)
	if err != nil {
		return nil, err
	}

	response := &SearchResponse{}
	for _, r := range results {
		totalMatches := countMatches(r.Matches)
		response.Results = append(response.Results, &SearchResult{
			Slug:           r.Extension.GetSlug(),
			Name:           r.Extension.GetName(),
			Version:        r.Extension.GetVersion(),
			ActiveInstalls: r.Extension.GetActiveInstalls(),
			Downloaded:     r.Extension.GetDownloaded(),
			Matches:        r.Matches,
			TotalMatches:   totalMatches,
		})
	}

	// Sort results by active_installs descending, then by slug for consistency
	sort.Slice(response.Results, func(i, j int) bool {
		if response.Results[i].ActiveInstalls != response.Results[j].ActiveInstalls {
			return response.Results[i].ActiveInstalls > response.Results[j].ActiveInstalls
		}
		return response.Results[i].Slug < response.Results[j].Slug
	})

	response.Total = len(response.Results)
	return response, nil
}

// countMatches counts the total number of line matches across all file matches.
func countMatches(matches []*index.FileMatch) int {
	total := 0
	for _, fm := range matches {
		total += len(fm.Matches)
	}
	return total
}
