package repo

import "veloria/internal/index"

// DataSource abstracts what the Manager needs from each extension store.
// Defined in the repo package (where the implementations live) rather than
// at the consumer site, because all implementations share the generic
// ExtensionStore[T] base.
type DataSource interface {
	Type() ExtensionType
	Load() error
	Stats() (total int, indexed int)
	IndexStatus() map[string]bool
	Search(term string, opt *index.SearchOptions, progressFn func(searched, total int)) ([]*SearchResult, error)
	PrepareUpdates() ([]IndexTask, error)
	ResumeUnindexed() []IndexTask
	GetExtension(slug string) (Extension, bool)
	MakeReindexTaskBySlug(slug string) (IndexTask, bool)
	ResolveSourceDir(slug string) (string, error)
}
