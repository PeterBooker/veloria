package cache

import "time"

// Cache defines the interface for an in-memory cache.
type Cache interface {
	// Get retrieves a value by key. Returns the value and true if found.
	Get(key string) (any, bool)

	// Set stores a value with a TTL. A zero TTL means no expiration.
	Set(key string, value any, ttl time.Duration)

	// Delete removes a value by key.
	Delete(key string)

	// Close releases any resources held by the cache.
	Close()
}
