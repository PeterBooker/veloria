package testutil

import (
	"context"

	"veloria/internal/index"
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
	SearchFunc           func(ctx context.Context, term string, opt *index.SearchOptions, fn func(int, int)) ([]*repo.SearchResult, error)
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

func (f *FakeDataSource) Search(ctx context.Context, term string, opt *index.SearchOptions, fn func(int, int)) ([]*repo.SearchResult, error) {
	if f.SearchFunc != nil {
		return f.SearchFunc(ctx, term, opt, fn)
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

