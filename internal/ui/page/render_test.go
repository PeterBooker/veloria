package page

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"regexp"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"veloria/internal/ui"
	"veloria/internal/user"
)

// markerSlug is an inert string with the characters that end a JSON string,
// an HTML attribute value and a script element.
const markerSlug = `x'y"z</script>w`

var (
	hxValsRe = regexp.MustCompile(`\shx-vals="([^"]*)"`)
	// downloadRe matches the download anchor, the only one on the page that
	// opens in a new tab with rel="noopener".
	downloadRe = regexp.MustCompile(`<a href="([^"]*)"[^>]*target="_blank" rel="noopener">`)
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, c.Render(context.Background(), &buf))
	return buf.String()
}

func TestExtension_ReindexValsAreJSON(t *testing.T) {
	out := render(t, Extension(ui.ExtensionData{
		PageData:       ui.PageData{User: &user.User{Name: "Admin", IsAdmin: true}},
		DataSourceType: "plugins",
		Name:           "Marker",
		Slug:           markerSlug,
		Version:        "1.0",
	}))

	assert.NotContains(t, out, markerSlug)
	assert.NotContains(t, out, `</script>w`)

	m := hxValsRe.FindAllStringSubmatch(out, -1)
	require.Len(t, m, 1)
	var vals map[string]string
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(m[0][1])), &vals))
	assert.Equal(t, map[string]string{"repo_type": "plugins", "slug": markerSlug}, vals)
}

func TestExtension_DownloadLinkScheme(t *testing.T) {
	tests := []struct {
		name     string
		dsType   string
		link     string
		wantHref string
	}{
		{"https plugin link", "plugins", "https://downloads.example.org/plugin/marker.1.0.zip", "https://downloads.example.org/plugin/marker.1.0.zip"},
		{"https core link", "cores", "https://wordpress.org/wordpress-6.4.2.zip", "https://wordpress.org/wordpress-6.4.2.zip"},
		// Handlers drop these before rendering; the template must still not
		// emit the scheme if one gets through.
		{"javascript plugin link", "plugins", "javascript:marker", string(templ.FailedSanitizationURL)},
		{"data theme link", "themes", "data:text/plain,marker", string(templ.FailedSanitizationURL)},
		{"javascript core link", "cores", "JavaScript:marker", string(templ.FailedSanitizationURL)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := render(t, Extension(ui.ExtensionData{
				DataSourceType: tt.dsType,
				Name:           "Marker",
				Slug:           "marker",
				Version:        "1.0",
				DownloadLink:   tt.link,
			}))

			m := downloadRe.FindAllStringSubmatch(out, -1)
			require.Len(t, m, 1)
			assert.Equal(t, tt.wantHref, html.UnescapeString(m[0][1]))
		})
	}
}

func TestExtension_NoDownloadLinkRendersNoAnchor(t *testing.T) {
	for _, dsType := range []string{"plugins", "themes", "cores"} {
		out := render(t, Extension(ui.ExtensionData{
			DataSourceType: dsType,
			Name:           "Marker",
			Slug:           "marker",
			Version:        "1.0",
		}))

		assert.Empty(t, downloadRe.FindAllString(out, -1), dsType)
		assert.Contains(t, out, `>Details</h3>`, dsType)
	}
}
