package manager

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"veloria/internal/index"
	"veloria/internal/repo"
)

type Manager struct {
	pr *repo.PluginRepo
	tr *repo.ThemeRepo
	cr *repo.CoreRepo
}

// NewManager creates a new Manager, initializes all repositories, and starts
// a single shared updater loop that pulls work from all three repos.
func NewManager(ctx context.Context, l *zerolog.Logger, pr *repo.PluginRepo, tr *repo.ThemeRepo, cr *repo.CoreRepo, concurrency int) (*Manager, error) {
	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)

	loadRepo := func(name string, loadFn func() error) {
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
	wg.Add(3)
	go loadRepo("plugins", pr.Load)
	go loadRepo("themes", tr.Load)
	go loadRepo("cores", cr.Load)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	m := &Manager{pr: pr, tr: tr, cr: cr}
	m.startUpdater(ctx, l, concurrency)

	return m, nil
}

// startUpdater runs a single background loop that collects pending work from
// all three repos and executes it through a shared worker pool, giving you one
// place to control total concurrency via INDEXER_CONCURRENCY.
func (m *Manager) startUpdater(ctx context.Context, l *zerolog.Logger, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}

	go func() {
		sem := make(chan struct{}, concurrency)

		// Resume any extensions that were saved to DB but not indexed (e.g. interrupted runs).
		resumeTasks := m.pr.ResumeUnindexed()
		resumeTasks = append(resumeTasks, m.tr.ResumeUnindexed()...)
		resumeTasks = append(resumeTasks, m.cr.ResumeUnindexed()...)
		if len(resumeTasks) > 0 {
			l.Info().Msgf("Resuming %d unindexed extensions", len(resumeTasks))
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
				l.Info().Msg("Stopping indexer updater")
				return
			default:
			}

			// Collect tasks from all repo types
			var tasks []repo.IndexTask
			tasks = append(tasks, m.pr.PrepareUpdates()...)
			tasks = append(tasks, m.tr.PrepareUpdates()...)
			tasks = append(tasks, m.cr.PrepareUpdates()...)

			if len(tasks) > 0 {
				l.Info().Msgf("Processing %d index tasks with concurrency %d", len(tasks), concurrency)

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
				l.Info().Msg("No pending updates")
			}

			select {
			case <-time.After(repo.UpdateInterval):
			case <-ctx.Done():
				l.Info().Msg("Stopping indexer updater")
				return
			}
		}
	}()
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
	OnProgress       ProgressFunc
}

// Search searches the specified repository for the given term.
func (m *Manager) Search(repoType string, term string, params *SearchParams) (*SearchResponse, error) {
	if params == nil {
		params = &SearchParams{}
	}

	opt := &index.SearchOptions{
		IgnoreCase:        params.CaseInsensitive,
		LiteralSearch:     false,
		FileRegexp:        params.FileMatch,
		ExcludeFileRegexp: params.ExcludeFileMatch,
		MaxResults:        100,
	}

	response := &SearchResponse{}

	switch repoType {
	case "plugins":
		results, err := m.pr.Search(term, opt, params.OnProgress)
		if err != nil {
			return nil, err
		}

		for _, r := range results {
			totalMatches := countMatches(r.Matches)
			response.Results = append(response.Results, &SearchResult{
				Slug:           r.Plugin.Slug,
				Name:           r.Plugin.Name,
				Version:        r.Plugin.Version,
				ActiveInstalls: r.Plugin.ActiveInstalls,
				Downloaded:     r.Plugin.Downloaded,
				Matches:        r.Matches,
				TotalMatches:   totalMatches,
			})
		}

	case "themes":
		results, err := m.tr.Search(term, opt, params.OnProgress)
		if err != nil {
			return nil, err
		}

		for _, r := range results {
			totalMatches := countMatches(r.Matches)
			response.Results = append(response.Results, &SearchResult{
				Slug:           r.Theme.Slug,
				Name:           r.Theme.Name,
				Version:        r.Theme.Version,
				ActiveInstalls: r.Theme.ActiveInstalls,
				Downloaded:     r.Theme.Downloaded,
				Matches:        r.Matches,
				TotalMatches:   totalMatches,
			})
		}

	case "cores":
		results, err := m.cr.Search(term, opt, params.OnProgress)
		if err != nil {
			return nil, err
		}

		for _, r := range results {
			totalMatches := countMatches(r.Matches)
			response.Results = append(response.Results, &SearchResult{
				Slug:           r.Core.Version,
				Name:           r.Core.Name,
				Version:        r.Core.Version,
				ActiveInstalls: 0, // Cores don't have install counts
				Downloaded:     0,
				Matches:        r.Matches,
				TotalMatches:   totalMatches,
			})
		}
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

// GetPluginRepo returns the plugin repository.
func (m *Manager) GetPluginRepo() *repo.PluginRepo {
	return m.pr
}

// GetThemeRepo returns the theme repository.
func (m *Manager) GetThemeRepo() *repo.ThemeRepo {
	return m.tr
}

// GetCoreRepo returns the core repository.
func (m *Manager) GetCoreRepo() *repo.CoreRepo {
	return m.cr
}
