package cache

import (
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

const (
	// defaultMaxCost is the memory budget of the cache in bytes.
	defaultMaxCost = 64 << 20

	// maxEntryFraction caps a single entry at MaxCost/maxEntryFraction bytes.
	// Larger values are not cached.
	maxEntryFraction = 16

	// defaultEntryCost is charged for values whose size is not known. It only
	// suits small fixed-size values; anything larger must be a []byte, a
	// string, or implement Sizer.
	defaultEntryCost = 1 << 10
)

// Sizer is implemented by cached values that can report their approximate
// size in bytes, so they are charged against the cache's memory budget.
type Sizer interface {
	CacheSize() int64
}

// RistrettoCache is a Cache backed by dgraph-io/ristretto.
type RistrettoCache struct {
	c            *ristretto.Cache[string, any]
	maxEntryCost int64
}

// NewRistretto creates a new ristretto-backed cache.
func NewRistretto() (*RistrettoCache, error) {
	return newRistretto(defaultMaxCost)
}

func newRistretto(maxCost int64) (*RistrettoCache, error) {
	c, err := ristretto.NewCache(&ristretto.Config[string, any]{
		NumCounters: 1e5, // track frequency for up to ~10K items
		MaxCost:     maxCost,
		BufferItems: 64,
	})
	if err != nil {
		return nil, err
	}
	return &RistrettoCache{c: c, maxEntryCost: maxCost / maxEntryFraction}, nil
}

func (r *RistrettoCache) Get(key string) (any, bool) {
	return r.c.Get(key)
}

// Set stores a value charged at its size in bytes. A value larger than the
// per-entry limit is not stored, and any previous value under key is removed.
func (r *RistrettoCache) Set(key string, value any, ttl time.Duration) {
	cost := entryCost(value)
	if cost > r.maxEntryCost {
		r.c.Del(key)
		return
	}
	r.c.SetWithTTL(key, value, cost, ttl)
}

func (r *RistrettoCache) Delete(key string) {
	r.c.Del(key)
}

func (r *RistrettoCache) Close() {
	r.c.Close()
}

// entryCost returns the number of bytes to charge for value.
func entryCost(value any) int64 {
	switch v := value.(type) {
	case []byte:
		return int64(cap(v))
	case string:
		return int64(len(v))
	case Sizer:
		return v.CacheSize()
	default:
		return defaultEntryCost
	}
}
