package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"veloria/internal/cache"
	"veloria/internal/index"
	"veloria/internal/repo"
	"veloria/internal/telemetry"
)

const (
	maxRetries       = 3
	stalenessWarning = 15 * time.Minute
)

// NotFoundError is a domain error indicating the requested extension was not found.
// Implements api.StatusCoder so the transport layer maps it to HTTP 404.
type NotFoundError struct {
	Msg string
}

func (e *NotFoundError) Error() string   { return e.Msg }
func (e *NotFoundError) StatusCode() int { return 404 }

// QueueFullError indicates the reindex queue is at capacity.
// Implements api.StatusCoder so the transport layer maps it to HTTP 429.
type QueueFullError struct{}

func (e *QueueFullError) Error() string   { return "reindex queue is full" }
func (e *QueueFullError) StatusCode() int { return 429 }

// Reindex error sentinels. Pointer identity is preserved for errors.Is checks.
var (
	ErrExtNotFound = &NotFoundError{Msg: "extension not found"}
	ErrQueueFull   = &QueueFullError{}
)

// retryEntry tracks a task waiting to be retried.
type retryEntry struct {
	task     repo.IndexTask
	attempts int
}

// retryKey uniquely identifies a retryable task.
type retryKey struct {
	repoType repo.ExtensionType
	slug     string
}

type Manager struct {
	sources     map[repo.ExtensionType]repo.DataSource
	sourceOrder []repo.ExtensionType // deterministic iteration order
	adhocCh     chan repo.IndexTask
	events      *repo.IndexEventRecorder // nil when DB unavailable
	done        chan struct{}            // closed when updater goroutine exits
	api         *repo.APIClient          // for health checks
	cache       cache.Cache              // shared app cache (may be nil)

	// Failure tracking (read by health endpoint, written by updater goroutine).
	mu                  sync.RWMutex
	consecutiveFailures map[repo.ExtensionType]int
	lastSuccessUpdate   map[repo.ExtensionType]time.Time
}

// NewManager creates a new Manager, initializes all data sources, and starts
// a single shared updater loop that pulls work from all sources.
func NewManager(ctx context.Context, l *zap.Logger, sources []repo.DataSource, concurrency int, events *repo.IndexEventRecorder, api *repo.APIClient, c cache.Cache) (*Manager, error) {
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
	order := make([]repo.ExtensionType, 0, len(sources))
	for _, ds := range sources {
		srcMap[ds.Type()] = ds
		order = append(order, ds.Type())
	}
	slices.Sort(order)

	m := &Manager{
		sources:             srcMap,
		sourceOrder:         order,
		adhocCh:             make(chan repo.IndexTask, 32),
		events:              events,
		done:                make(chan struct{}),
		api:                 api,
		cache:               c,
		consecutiveFailures: make(map[repo.ExtensionType]int),
		lastSuccessUpdate:   make(map[repo.ExtensionType]time.Time),
	}
	m.startUpdater(ctx, l, concurrency)

	return m, nil
}

// NewTestManager creates a Manager for testing without loading data or starting
// the updater goroutine. It is intentionally minimal so unit tests stay fast.
func NewTestManager(l *zap.Logger, sources []repo.DataSource) (*Manager, error) {
	srcMap := make(map[repo.ExtensionType]repo.DataSource, len(sources))
	order := make([]repo.ExtensionType, 0, len(sources))
	for _, ds := range sources {
		srcMap[ds.Type()] = ds
		order = append(order, ds.Type())
	}
	slices.Sort(order)

	return &Manager{
		sources:             srcMap,
		sourceOrder:         order,
		adhocCh:             make(chan repo.IndexTask, 32),
		done:                make(chan struct{}),
		consecutiveFailures: make(map[repo.ExtensionType]int),
		lastSuccessUpdate:   make(map[repo.ExtensionType]time.Time),
	}, nil
}

// GetSource returns the DataSource for the given extension type, or nil.
func (m *Manager) GetSource(t repo.ExtensionType) repo.DataSource {
	return m.sources[t]
}

// Wait blocks until the updater goroutine has exited.
func (m *Manager) Wait() {
	<-m.done
}

// startUpdater runs a single background loop that collects pending work from
// all sources and executes it through a shared worker pool, giving you one
// place to control total concurrency via INDEXER_CONCURRENCY.
func (m *Manager) startUpdater(ctx context.Context, l *zap.Logger, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}

	go func() {
		defer close(m.done)

		sem := make(chan struct{}, concurrency)
		retries := make(map[retryKey]*retryEntry)

		// Resume any extensions that were saved to DB but not indexed (e.g. interrupted runs).
		var resumeTasks []repo.IndexTask
		for _, t := range m.sourceOrder {
			resumeTasks = append(resumeTasks, m.sources[t].ResumeUnindexed()...)
		}
		if len(resumeTasks) > 0 {
			l.Info("Resuming unindexed extensions", zap.Int("count", len(resumeTasks)))
			m.runTasks(ctx, l, sem, resumeTasks)
		}

		for {
			select {
			case <-ctx.Done():
				l.Info("Stopping indexer updater")
				return
			default:
			}

			// Phase 1: Retry previously failed tasks.
			if len(retries) > 0 {
				var retryTasks []repo.IndexTask
				for _, entry := range retries {
					retryTasks = append(retryTasks, entry.task)
				}
				l.Info("Retrying failed tasks", zap.Int("count", len(retryTasks)))
				failures := m.runTasks(ctx, l, sem, retryTasks)

				// Build set of tasks that still failed.
				failedSet := make(map[retryKey]struct{})
				for _, f := range failures {
					failedSet[retryKey{repoType: f.ExtensionType, slug: f.Slug}] = struct{}{}
				}

				// Update retry map: remove successes, bump attempt count or evict.
				for k, entry := range retries {
					if _, stillFailed := failedSet[k]; !stillFailed {
						delete(retries, k) // succeeded on retry
						continue
					}
					entry.attempts++
					if entry.attempts >= maxRetries {
						l.Error("Task exceeded max retries, giving up",
							zap.String("type", string(k.repoType)),
							zap.String("slug", k.slug),
							zap.Int("attempts", entry.attempts),
						)
						delete(retries, k)
					}
				}
			}

			// Phase 2: Collect fresh tasks from all sources (deterministic order).
			var tasks []repo.IndexTask
			for _, t := range m.sourceOrder {
				ds := m.sources[t]
				dsTasks, err := ds.PrepareUpdates()
				if err != nil {
					m.trackFailure(t, l, err)
					continue
				}
				m.trackSuccess(t)
				tasks = append(tasks, dsTasks...)
			}

			// Check for staleness.
			m.checkStaleness(l)

			if len(tasks) > 0 {
				l.Info("Processing index tasks", zap.Int("count", len(tasks)), zap.Int("concurrency", concurrency))
				failures := m.runTasks(ctx, l, sem, tasks)

				// Add failures to retry map.
				for _, f := range failures {
					k := retryKey{repoType: f.ExtensionType, slug: f.Slug}
					if _, exists := retries[k]; !exists {
						retries[k] = &retryEntry{task: f.IndexTask, attempts: 1}
					}
				}
			} else {
				l.Info("No pending updates")
			}

			// Drain any ad-hoc re-index requests queued while processing the batch.
			m.drainAdhoc(ctx, l, sem)

			select {
			case <-time.After(repo.UpdateInterval):
			case task := <-m.adhocCh:
				l.Info("Ad-hoc re-index request", zap.String("type", string(task.ExtensionType)), zap.String("slug", task.Slug))
				m.runTasks(ctx, l, sem, []repo.IndexTask{task})
			case <-ctx.Done():
				l.Info("Stopping indexer updater")
				return
			}
		}
	}()
}

// failedTask identifies a task that returned an error.
type failedTask struct {
	repo.IndexTask
}

// runTasks executes tasks through the shared semaphore, recording events and
// returning any tasks that failed (for retry).
func (m *Manager) runTasks(ctx context.Context, l *zap.Logger, sem chan struct{}, tasks []repo.IndexTask) []failedTask {
	var (
		mu       sync.Mutex
		failures []failedTask
		wg       sync.WaitGroup
	)

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return failures
		default:
		}

		wg.Add(1)
		go func(t repo.IndexTask) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			err := t.Run()
			elapsed := time.Since(start)

			// Classify the outcome.
			skipped := errors.Is(err, repo.ErrDownloadSkipped)
			status := "success"
			if err != nil {
				if skipped {
					status = "skipped"
				} else {
					status = "failed"
				}
			}

			// Record Prometheus metrics.
			repoAttr := attribute.String("repo_type", string(t.ExtensionType))
			statusAttr := attribute.String("status", status)
			if telemetry.IndexingTasksTotal != nil {
				telemetry.IndexingTasksTotal.Add(ctx, 1, metric.WithAttributes(repoAttr, statusAttr))
			}
			if telemetry.IndexingTaskDuration != nil {
				telemetry.IndexingTaskDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(repoAttr))
			}

			// Record persistent event.
			if m.events != nil {
				event := repo.IndexEvent{
					RepoType:   string(t.ExtensionType),
					Slug:       t.Slug,
					DurationMS: elapsed.Milliseconds(),
				}
				switch {
				case skipped:
					event.Status = repo.IndexEventSkipped
					event.ErrorMessage = err.Error()
				case err != nil:
					event.Status = repo.IndexEventFailed
					event.ErrorMessage = err.Error()
				default:
					event.Status = repo.IndexEventSuccess
					// Clear stale failure events now that this slug succeeded.
					m.events.ClearFailures(string(t.ExtensionType), t.Slug)
				}
				m.events.Record(event)
			}

			// Update durable index state in DB.
			if ds := m.sources[t.ExtensionType]; ds != nil {
				switch {
				case skipped:
					// No state change for skipped tasks.
				case err != nil:
					ds.RecordIndexFailure(t.Slug)
				default:
					ds.RecordIndexSuccess(t.Slug)
				}
			}

			// Only count real failures for retry — skipped tasks are not retryable.
			if err != nil && !skipped {
				mu.Lock()
				failures = append(failures, failedTask{t})
				mu.Unlock()
			}
		}(task)
	}

	wg.Wait()
	return failures
}

// drainAdhoc processes any pending ad-hoc re-index tasks from the channel.
func (m *Manager) drainAdhoc(ctx context.Context, l *zap.Logger, sem chan struct{}) {
	for {
		select {
		case task := <-m.adhocCh:
			m.runTasks(ctx, l, sem, []repo.IndexTask{task})
		default:
			return
		}
	}
}

// trackFailure increments consecutive failure count and logs at escalating severity.
func (m *Manager) trackFailure(t repo.ExtensionType, l *zap.Logger, err error) {
	m.mu.Lock()
	m.consecutiveFailures[t]++
	count := m.consecutiveFailures[t]
	m.mu.Unlock()

	if telemetry.ConsecutiveFailures != nil {
		telemetry.ConsecutiveFailures.Record(context.Background(), int64(count),
			metric.WithAttributes(attribute.String("repo_type", string(t))))
	}

	if count <= 2 {
		l.Warn("PrepareUpdates failed",
			zap.String("type", string(t)), zap.Int("consecutive", count), zap.Error(err))
	} else {
		l.Error("PrepareUpdates failed repeatedly",
			zap.String("type", string(t)), zap.Int("consecutive", count), zap.Error(err))
	}
}

// trackSuccess resets consecutive failure count and records last success time.
func (m *Manager) trackSuccess(t repo.ExtensionType) {
	m.mu.Lock()
	m.consecutiveFailures[t] = 0
	m.lastSuccessUpdate[t] = time.Now()
	m.mu.Unlock()

	if telemetry.ConsecutiveFailures != nil {
		telemetry.ConsecutiveFailures.Record(context.Background(), 0,
			metric.WithAttributes(attribute.String("repo_type", string(t))))
	}
	if telemetry.LastSuccessfulUpdateTS != nil {
		telemetry.LastSuccessfulUpdateTS.Record(context.Background(), float64(time.Now().Unix()),
			metric.WithAttributes(attribute.String("repo_type", string(t))))
	}
}

// checkStaleness logs a warning if any data source hasn't had a successful
// update in longer than the staleness threshold.
func (m *Manager) checkStaleness(l *zap.Logger) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, t := range m.sourceOrder {
		last, ok := m.lastSuccessUpdate[t]
		if !ok {
			continue // never succeeded yet — startup phase
		}
		if time.Since(last) > stalenessWarning {
			l.Warn("Data source may be stale",
				zap.String("type", string(t)),
				zap.Duration("since_last_success", time.Since(last)),
			)
		}
	}
}

// SourceHealth returns the consecutive failure count and last successful
// update time for each data source. Used by the health endpoint.
func (m *Manager) SourceHealth() map[string]SourceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]SourceStatus, len(m.sourceOrder))
	for _, t := range m.sourceOrder {
		result[string(t)] = SourceStatus{
			ConsecutiveFailures: m.consecutiveFailures[t],
			LastSuccess:         m.lastSuccessUpdate[t],
		}
	}
	return result
}

// SourceStatus holds health information about a single data source.
type SourceStatus struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastSuccess         time.Time `json:"last_success"`
}

// SubmitReindex queues an ad-hoc re-index task for the given extension.
// Returns an error if the extension is not found or the queue is full.
func (m *Manager) SubmitReindex(repoType, slug string) error {
	ds, ok := m.sources[repo.ExtensionType(repoType)]
	if !ok {
		return ErrExtNotFound
	}

	task, found := ds.MakeReindexTaskBySlug(slug)
	if !found {
		return ErrExtNotFound
	}

	select {
	case m.adhocCh <- task:
		return nil
	default:
		return ErrQueueFull
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

// BreakerState returns the circuit breaker state, or empty string if unavailable.
func (m *Manager) BreakerState() string {
	if m.api != nil {
		return m.api.BreakerState()
	}
	return ""
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

// searchCacheKey builds a cache key for a search query.
func searchCacheKey(repoType string, term string, params *SearchParams) string {
	return fmt.Sprintf("search:%s:%s:%v:%s:%s:%d",
		repoType, term, params.CaseInsensitive,
		params.FileMatch, params.ExcludeFileMatch, params.LinesOfContext)
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

	// Check cache for identical query.
	cacheKey := searchCacheKey(repoType, term, params)
	if m.cache != nil {
		if v, ok := m.cache.Get(cacheKey); ok {
			resp := v.(*SearchResponse)
			if params.OnProgress != nil {
				params.OnProgress(resp.Total, resp.Total)
			}
			return resp, nil
		}
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

	if m.cache != nil {
		m.cache.Set(cacheKey, response, 60*time.Second)
	}

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
