package cache

import (
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

// RistrettoCache is a Cache backed by dgraph-io/ristretto.
type RistrettoCache struct {
	c *ristretto.Cache[string, any]
}

// NewRistretto creates a new ristretto-backed cache.
func NewRistretto() (*RistrettoCache, error) {
	c, err := ristretto.NewCache(&ristretto.Config[string, any]{
		NumCounters: 1e5, // track frequency for up to ~10K items
		MaxCost:     64 << 20,
		BufferItems: 64,
	})
	if err != nil {
		return nil, err
	}
	return &RistrettoCache{c: c}, nil
}

func (r *RistrettoCache) Get(key string) (any, bool) {
	return r.c.Get(key)
}

func (r *RistrettoCache) Set(key string, value any, ttl time.Duration) {
	r.c.SetWithTTL(key, value, 1, ttl)
}

func (r *RistrettoCache) Delete(key string) {
	r.c.Del(key)
}

func (r *RistrettoCache) Close() {
	r.c.Close()
}
