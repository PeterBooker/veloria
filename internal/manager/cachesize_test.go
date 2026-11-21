package manager_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"veloria/internal/index"
	"veloria/internal/manager"
)

func TestSearchResponse_CacheSize(t *testing.T) {
	match := func(lineLen, contextLines int) *index.Match {
		m := &index.Match{Line: strings.Repeat("x", lineLen), LineNumber: 1}
		for range contextLines {
			m.Before = append(m.Before, strings.Repeat("b", lineLen))
			m.After = append(m.After, strings.Repeat("a", lineLen))
		}
		return m
	}
	response := func(matches, lineLen, contextLines int) *manager.SearchResponse {
		fm := &index.FileMatch{Filename: "includes/file.php"}
		for range matches {
			fm.Matches = append(fm.Matches, match(lineLen, contextLines))
		}
		return &manager.SearchResponse{
			Total:   1,
			Results: []*manager.SearchResult{{Slug: "slug", Name: "Name", Version: "1.0", Matches: []*index.FileMatch{fm}}},
		}
	}

	var nilResponse *manager.SearchResponse
	assert.Zero(t, nilResponse.CacheSize())

	empty := (&manager.SearchResponse{}).CacheSize()
	assert.Positive(t, empty)

	base := response(10, 100, 0).CacheSize()
	assert.Greater(t, base, empty)
	assert.GreaterOrEqual(t, base, int64(10*100), "estimate covers the matched line bytes")

	// The estimate grows with the match count, the line lengths, and context lines.
	assert.Greater(t, response(20, 100, 0).CacheSize(), base)
	assert.Greater(t, response(10, 200, 0).CacheSize(), base)
	withContext := response(10, 100, 2).CacheSize()
	assert.GreaterOrEqual(t, withContext, base+int64(10*4*100))

	// A large response is charged megabytes, not a constant.
	assert.GreaterOrEqual(t, response(10000, 500, 0).CacheSize(), int64(10000*500))
}
