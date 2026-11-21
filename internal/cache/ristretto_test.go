package cache_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"veloria/internal/cache"
	"veloria/internal/index"
	"veloria/internal/manager"
)

// testMaxCost is a small byte budget so the tests do not allocate the
// production 64 MiB. The per-entry limit is testMaxCost/16 = 64 KiB.
const (
	testMaxCost      = 1 << 20
	testMaxEntryCost = testMaxCost / 16
)

func newCache(t *testing.T) *cache.RistrettoCache {
	t.Helper()
	c, err := cache.NewRistrettoWithMaxCost(testMaxCost)
	if err != nil {
		t.Fatalf("creating cache: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// sized is a value that reports a fixed size.
type sized int64

func (s sized) CacheSize() int64 { return int64(s) }

// internalCost returns what the cache charges for an entry on top of the cost
// passed by Set, measured with a zero-length value.
func internalCost(t *testing.T) int64 {
	t.Helper()
	c := newCache(t)
	c.Set("empty", "", time.Hour)
	c.Wait()
	return c.UsedCost()
}

func TestRistretto_RetainedBytesStayWithinMaxCost(t *testing.T) {
	t.Parallel()

	const entrySize = 16 << 10
	// Three times the budget, under distinct keys.
	const entries = 3 * testMaxCost / entrySize

	tests := []struct {
		name  string
		value func() any
		size  func(v any) int
	}{
		{
			name:  "bytes",
			value: func() any { return make([]byte, entrySize) },
			size:  func(v any) int { return len(v.([]byte)) },
		},
		{
			name:  "string",
			value: func() any { return strings.Repeat("x", entrySize) },
			size:  func(v any) int { return len(v.(string)) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newCache(t)

			key := func(i int) string { return fmt.Sprintf("og:00000000-0000-4000-8000-%012d", i) }
			for i := range entries {
				c.Set(key(i), tt.value(), 24*time.Hour)
				if i%32 == 31 {
					c.Wait()
				}
			}
			c.Wait()

			var kept, keptBytes int
			for i := range entries {
				if v, ok := c.Get(key(i)); ok {
					kept++
					keptBytes += tt.size(v)
				}
			}

			t.Logf("stored %d entries (%d bytes), retained %d entries (%d bytes), MaxCost %d",
				entries, entries*entrySize, kept, keptBytes, testMaxCost)
			if keptBytes > testMaxCost {
				t.Errorf("retained %d bytes, above MaxCost %d", keptBytes, testMaxCost)
			}
			if kept == 0 {
				t.Error("no entry retained; the cache should keep entries up to its budget")
			}
			if used := c.UsedCost(); used > testMaxCost {
				t.Errorf("charged cost %d is above MaxCost %d", used, testMaxCost)
			}
		})
	}
}

func TestRistretto_OversizedEntryRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  any
		stored bool
	}{
		{"bytes at the limit", make([]byte, testMaxEntryCost), true},
		{"bytes above the limit", make([]byte, testMaxEntryCost+1), false},
		{"string above the limit", strings.Repeat("x", testMaxEntryCost+1), false},
		{"sized value at the limit", sized(testMaxEntryCost), true},
		{"sized value above the limit", sized(testMaxEntryCost + 1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newCache(t)

			c.Set("key", tt.value, time.Hour)
			c.Wait()

			if _, ok := c.Get("key"); ok != tt.stored {
				t.Errorf("stored = %v, want %v", ok, tt.stored)
			}
		})
	}

	t.Run("refused value removes the previous one", func(t *testing.T) {
		t.Parallel()
		c := newCache(t)

		c.Set("key", []byte("small"), time.Hour)
		c.Wait()
		if _, ok := c.Get("key"); !ok {
			t.Fatal("small value was not stored")
		}

		c.Set("key", make([]byte, testMaxEntryCost+1), time.Hour)
		c.Wait()
		if _, ok := c.Get("key"); ok {
			t.Error("stale value still served after an oversized Set")
		}
	})
}

func TestRistretto_ChargesEntriesBySize(t *testing.T) {
	t.Parallel()

	response := &manager.SearchResponse{
		Total: 1,
		Results: []*manager.SearchResult{{
			Slug:    "akismet",
			Name:    "Akismet Anti-spam",
			Version: "5.3",
			Matches: []*index.FileMatch{{
				Filename: "akismet.php",
				Matches: []*index.Match{
					{Line: strings.Repeat("a", 2000), LineNumber: 10},
					{Line: strings.Repeat("b", 3000), LineNumber: 20, Before: []string{strings.Repeat("c", 500)}},
				},
			}},
		}},
	}

	tests := []struct {
		name  string
		value any
		want  int64
	}{
		{"bytes", make([]byte, 1000), 1000},
		{"bytes with spare capacity", make([]byte, 10, 3000), 3000},
		{"string", strings.Repeat("x", 2000), 2000},
		{"sized value", sized(12345), 12345},
		{"search response", response, response.CacheSize()},
		{"other type", [2]int{1, 2}, 1 << 10},
	}

	overhead := internalCost(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := newCache(t)

			c.Set("key", tt.value, time.Hour)
			c.Wait()

			if _, ok := c.Get("key"); !ok {
				t.Fatal("value was not stored")
			}
			if got := c.UsedCost() - overhead; got != tt.want {
				t.Errorf("charged %d bytes, want %d", got, tt.want)
			}
		})
	}
}
