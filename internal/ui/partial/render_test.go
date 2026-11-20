package partial

import (
	"bytes"
	"context"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"veloria/internal/index"
	"veloria/internal/manager"
	"veloria/internal/ui"
)

const (
	// markerSlug is an inert string with the characters that end a JavaScript
	// string literal, an HTML attribute value and a script element.
	markerSlug = `x'y"z</script>w`
	// markerQuery has the characters that split or end a query string.
	markerQuery  = `a&b=c d#e+f`
	testSearchID = "0190a5c8-1f2e-7c3a-9b4d-5e6f708192a3"
)

// attrRe matches one attribute in templ output, which always writes attribute
// values in double quotes with any double quote in the value escaped.
var attrRe = regexp.MustCompile(`\s([a-zA-Z][a-zA-Z0-9:_-]*)="([^"]*)"`)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, c.Render(context.Background(), &buf))
	return buf.String()
}

// attrs returns the decoded values of every attribute in out, by name.
func attrs(out string) map[string][]string {
	found := map[string][]string{}
	for _, m := range attrRe.FindAllStringSubmatch(out, -1) {
		found[m[1]] = append(found[m[1]], html.UnescapeString(m[2]))
	}
	return found
}

func parseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func TestSearchExtensions_SlugStaysOutOfHandlers(t *testing.T) {
	out := render(t, SearchExtensions(ui.SearchExtensionsData{
		SearchID:         testSearchID,
		SearchDataSource: "plugins",
		Extensions: []ui.ExtensionResultSummary{
			{Slug: markerSlug, Name: "Marker", Version: "1.0", TotalMatches: 1},
			{Slug: "plain", Name: "Plain", Version: "2.0", TotalMatches: 2},
		},
		Page:       1,
		TotalPages: 1,
	}))
	found := attrs(out)

	// The slug never reaches the response unescaped.
	assert.NotContains(t, out, markerSlug)
	assert.NotContains(t, out, `x'y`)
	assert.NotContains(t, out, `</script>`)

	// Event handlers are static: the same text for every row, with no part
	// of either slug in it.
	handlers := 0
	for name, values := range found {
		if !strings.HasPrefix(name, "on") {
			continue
		}
		handlers++
		require.Len(t, values, 2, name)
		assert.Equal(t, values[0], values[1], "%s differs between rows", name)
		for _, frag := range []string{`x'y`, `y"z`, "script", "x%27y", "plain"} {
			assert.NotContains(t, values[0], frag, name)
		}
	}
	assert.Equal(t, 2, handlers, "expected onclick and onkeydown")

	// The slug is carried by a data attribute, as one escaped path segment.
	urls := found["data-ext-url"]
	require.Len(t, urls, 2)
	u := parseURL(t, urls[0])
	assert.Empty(t, u.RawQuery)
	assert.Empty(t, u.Fragment)
	segments := strings.Split(u.EscapedPath(), "/")
	require.Equal(t, []string{"", "search", testSearchID, "extension"}, segments[:len(segments)-1])
	slug, err := url.PathUnescape(segments[len(segments)-1])
	require.NoError(t, err)
	assert.Equal(t, markerSlug, slug)
	assert.Equal(t, "/search/"+testSearchID+"/extension/plain", urls[1])

	// Each button points at the detail panel of its own row, and the IDs
	// hold no upstream data.
	assert.Equal(t, []string{"ext-detail-0", "ext-detail-1"}, found["aria-controls"])
	assert.Subset(t, found["id"], found["aria-controls"])
}

func TestSearchExtensions_PaginationEscapesFilter(t *testing.T) {
	out := render(t, SearchExtensions(ui.SearchExtensionsData{
		SearchID:         testSearchID,
		SearchDataSource: "plugins",
		Extensions:       []ui.ExtensionResultSummary{{Slug: "plain", Name: "Plain", Version: "1.0"}},
		Page:             2,
		TotalPages:       3,
		Search:           markerQuery,
	}))

	links := attrs(out)["hx-get"]
	require.Len(t, links, 2)
	for i, wantPage := range []string{"1", "3"} {
		u := parseURL(t, links[i])
		assert.Equal(t, "/search/"+testSearchID+"/extensions", u.Path)
		assert.Empty(t, u.Fragment)
		assert.Equal(t, url.Values{"page": {wantPage}, "search": {markerQuery}}, u.Query())
	}
}

func TestSearchExtensionResults_ContextURLEscaped(t *testing.T) {
	const filename = `inc/a&b=c #d+e.php`
	out := render(t, SearchExtensionResults(ui.ExtensionResultsData{
		SearchID:         testSearchID,
		SearchDataSource: "plugins",
		Result: &manager.SearchResult{
			Slug: markerSlug,
			Matches: []*index.FileMatch{{
				Filename: filename,
				Matches:  []*index.Match{{Line: "marker line", LineNumber: 7}},
			}},
		},
	}))

	assert.NotContains(t, out, markerSlug)
	assert.NotContains(t, out, `</script>`)

	links := attrs(out)["hx-get"]
	require.Len(t, links, 1)
	u := parseURL(t, links[0])
	assert.Equal(t, "/search/"+testSearchID+"/context", u.Path)
	assert.Empty(t, u.Fragment)
	assert.Equal(t, url.Values{
		"repo": {"plugins"},
		"slug": {markerSlug},
		"file": {filename},
		"line": {"7"},
	}, u.Query())
}

func TestDataSourceItems_PaginationEscapesFilter(t *testing.T) {
	out := render(t, DataSourceItems(ui.DataSourceItemsData{
		DataSource: "plugins",
		Items:      []ui.DataSourceItem{{Name: "Plain", Slug: "plain", Version: "1.0"}},
		Page:       2,
		TotalPages: 3,
		Search:     markerQuery,
	}))

	links := attrs(out)["hx-get"]
	require.Len(t, links, 2)
	for i, wantPage := range []string{"1", "3"} {
		u := parseURL(t, links[i])
		assert.Equal(t, "/data-sources/plugins/items", u.Path)
		assert.Empty(t, u.Fragment)
		assert.Equal(t, url.Values{"page": {wantPage}, "search": {markerQuery}}, u.Query())
	}
}
