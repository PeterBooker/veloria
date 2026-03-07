package repo

import (
	"context"

	"veloria/internal/index"
)

// DataSource abstracts what the Manager needs from each extension store.
// Defined in the repo package (where the implementations live) rather than
// at the consumer site, because all implementations share the generic
// ExtensionStore[T] base.
type DataSource interface {
	Type() ExtensionType
	Load() error
	Stats() (total int, indexed int)
	IndexStatus() map[string]bool
	Search(ctx context.Context, term string, opt *index.SearchOptions, progressFn func(searched, total int)) ([]*SearchResult, int, error)
	PrepareUpdates() ([]IndexTask, error)
	ResumeUnindexed() []IndexTask
	GetExtension(slug string) (Extension, bool)
	MakeReindexTaskBySlug(slug string) (IndexTask, bool)
	ResolveSourceDir(slug string) (string, error)
	RecordIndexSuccess(slug string)
	RecordIndexFailure(slug string)
}
