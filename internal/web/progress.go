package web

import (
	"sync"

	"github.com/google/uuid"
)

// SearchProgress holds the current progress of an in-flight search.
type SearchProgress struct {
	Searched int
	Total    int
}

// ProgressStore is a concurrent-safe in-memory store for tracking search progress.
type ProgressStore struct {
	store sync.Map
}

// Set updates the progress for a search.
func (ps *ProgressStore) Set(id uuid.UUID, searched, total int) {
	ps.store.Store(id, SearchProgress{Searched: searched, Total: total})
}

// Get returns the current progress for a search, if it exists.
func (ps *ProgressStore) Get(id uuid.UUID) (SearchProgress, bool) {
	v, ok := ps.store.Load(id)
	if !ok {
		return SearchProgress{}, false
	}
	return v.(SearchProgress), true
}

// Delete removes progress tracking for a completed/failed search.
func (ps *ProgressStore) Delete(id uuid.UUID) {
	ps.store.Delete(id)
}
