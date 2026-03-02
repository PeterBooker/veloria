package testutil

import (
	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/repo"
)

// FakeDataSource is a hand-written fake implementing repo.DataSource.
// Each method delegates to a function field when set, making it easy to
// customise behaviour per-test without a mocking framework.
type FakeDataSource struct {
	TypeVal              repo.ExtensionType
	LoadFunc             func() error
	StatsFunc            func() (int, int)
	IndexStatusFunc      func() map[string]bool
	SearchFunc           func(term string, opt *index.SearchOptions, fn func(int, int)) ([]*repo.SearchResult, error)
	PrepareUpdatesFunc   func() ([]repo.IndexTask, error)
	ResumeUnindexedFunc  func() []repo.IndexTask
	GetExtensionFunc     func(slug string) (repo.Extension, bool)
	MakeReindexTaskFunc  func(slug string) (repo.IndexTask, bool)
	ResolveSourceDirFunc func(slug string) (string, error)
}

func (f *FakeDataSource) Type() repo.ExtensionType { return f.TypeVal }

func (f *FakeDataSource) Load() error {
	if f.LoadFunc != nil {
		return f.LoadFunc()
	}
	return nil
}

func (f *FakeDataSource) Stats() (int, int) {
	if f.StatsFunc != nil {
		return f.StatsFunc()
	}
	return 0, 0
}

func (f *FakeDataSource) IndexStatus() map[string]bool {
	if f.IndexStatusFunc != nil {
		return f.IndexStatusFunc()
	}
	return nil
}

func (f *FakeDataSource) Search(term string, opt *index.SearchOptions, fn func(int, int)) ([]*repo.SearchResult, error) {
	if f.SearchFunc != nil {
		return f.SearchFunc(term, opt, fn)
	}
	return nil, nil
}

func (f *FakeDataSource) PrepareUpdates() ([]repo.IndexTask, error) {
	if f.PrepareUpdatesFunc != nil {
		return f.PrepareUpdatesFunc()
	}
	return nil, nil
}

func (f *FakeDataSource) ResumeUnindexed() []repo.IndexTask {
	if f.ResumeUnindexedFunc != nil {
		return f.ResumeUnindexedFunc()
	}
	return nil
}

func (f *FakeDataSource) GetExtension(slug string) (repo.Extension, bool) {
	if f.GetExtensionFunc != nil {
		return f.GetExtensionFunc(slug)
	}
	return nil, false
}

func (f *FakeDataSource) MakeReindexTaskBySlug(slug string) (repo.IndexTask, bool) {
	if f.MakeReindexTaskFunc != nil {
		return f.MakeReindexTaskFunc(slug)
	}
	return repo.IndexTask{}, false
}

func (f *FakeDataSource) ResolveSourceDir(slug string) (string, error) {
	if f.ResolveSourceDirFunc != nil {
		return f.ResolveSourceDirFunc(slug)
	}
	return "", nil
}

func (f *FakeDataSource) RecordIndexSuccess(_ string) {}
func (f *FakeDataSource) RecordIndexFailure(_ string) {}

// Compile-time interface check.
var _ repo.DataSource = (*FakeDataSource)(nil)

// FakeSearchService is a hand-written fake implementing web.SearchService.
type FakeSearchService struct {
	SearchFunc func(repoType string, term string, params *manager.SearchParams) (*manager.SearchResponse, error)
}

func (f *FakeSearchService) Search(repoType string, term string, params *manager.SearchParams) (*manager.SearchResponse, error) {
	if f.SearchFunc != nil {
		return f.SearchFunc(repoType, term, params)
	}
	return &manager.SearchResponse{}, nil
}

// FakeReindexService is a hand-written fake implementing web.ReindexService.
type FakeReindexService struct {
	SubmitReindexFunc func(repoType, slug string) error
}

func (f *FakeReindexService) SubmitReindex(repoType, slug string) error {
	if f.SubmitReindexFunc != nil {
		return f.SubmitReindexFunc(repoType, slug)
	}
	return nil
}

// FakeSourceResolver is a hand-written fake implementing web.SourceResolver.
type FakeSourceResolver struct {
	ResolveSourceDirFunc func(repoType, slug string) (string, error)
}

func (f *FakeSourceResolver) ResolveSourceDir(repoType, slug string) (string, error) {
	if f.ResolveSourceDirFunc != nil {
		return f.ResolveSourceDirFunc(repoType, slug)
	}
	return "", nil
}

// FakeStatsProvider is a hand-written fake implementing web.StatsProvider.
type FakeStatsProvider struct {
	StatsFunc       func(repoType string) (int, int, bool)
	IndexStatusFunc func(repoType string) map[string]bool
}

func (f *FakeStatsProvider) Stats(repoType string) (int, int, bool) {
	if f.StatsFunc != nil {
		return f.StatsFunc(repoType)
	}
	return 0, 0, false
}

func (f *FakeStatsProvider) IndexStatus(repoType string) map[string]bool {
	if f.IndexStatusFunc != nil {
		return f.IndexStatusFunc(repoType)
	}
	return nil
}
