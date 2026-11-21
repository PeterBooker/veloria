package web

import (
	"bytes"
	"context"
	"html"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"veloria/internal/ui/page"
)

func TestSafeDownloadLink(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"https", "https://downloads.example.org/plugin/marker.1.0.zip", "https://downloads.example.org/plugin/marker.1.0.zip"},
		{"http", "http://downloads.example.org/marker.zip", "http://downloads.example.org/marker.zip"},
		{"upper-case scheme", "HTTPS://downloads.example.org/marker.zip", "HTTPS://downloads.example.org/marker.zip"},
		{"query and fragment", "https://example.org/d?f=marker.zip#top", "https://example.org/d?f=marker.zip#top"},

		{"empty", "", ""},
		{"javascript", "javascript:marker", ""},
		{"javascript mixed case", "JavaScript:marker", ""},
		{"javascript with slashes", "javascript://example.org/%0Amarker", ""},
		{"data", "data:text/plain,marker", ""},
		{"vbscript", "vbscript:marker", ""},
		{"file", "file:///etc/hostname", ""},
		{"ftp", "ftp://example.org/marker.zip", ""},
		{"scheme-relative", "//example.org/marker.zip", ""},
		{"relative path", "/downloads/marker.zip", ""},
		{"no host", "https:///marker.zip", ""},
		{"opaque https", "https:example.org/marker.zip", ""},
		{"backslashes", `https:\\example.org\marker.zip`, ""},
		{"leading space", " https://example.org/marker.zip", ""},
		{"leading tab before scheme", "\tjavascript:marker", ""},
		{"tab inside scheme", "java\tscript:marker", ""},
		{"newline in host", "https://exam\nple.org/marker.zip", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SafeDownloadLink(tt.raw))
		})
	}
}

// TestSafeDownloadLink_RenderedAnchor renders the extension page the way the
// plugin and theme handlers do, with the stored link passed through
// SafeDownloadLink first.
func TestSafeDownloadLink_RenderedAnchor(t *testing.T) {
	// The download anchor is the only one on the page that opens in a new
	// tab with rel="noopener".
	downloadRe := regexp.MustCompile(`<a href="([^"]*)"[^>]*target="_blank" rel="noopener">`)

	tests := []struct {
		name     string
		stored   string
		wantHref string // "" means no anchor
	}{
		{"https", "https://downloads.example.org/plugin/marker.1.0.zip", "https://downloads.example.org/plugin/marker.1.0.zip"},
		{"javascript", "javascript:marker", ""},
		{"data", "data:text/plain,marker", ""},
	}
	for _, dsType := range []string{"plugins", "themes"} {
		for _, tt := range tests {
			t.Run(dsType+" "+tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				require.NoError(t, page.Extension(ExtensionData{
					DataSourceType: dsType,
					Name:           "Marker",
					Slug:           "marker",
					Version:        "1.0",
					DownloadLink:   SafeDownloadLink(tt.stored),
				}).Render(context.Background(), &buf))
				out := buf.String()

				m := downloadRe.FindAllStringSubmatch(out, -1)
				if tt.wantHref == "" {
					assert.Empty(t, m)
					assert.NotContains(t, out, "marker</a>")
					assert.NotContains(t, out, tt.stored)
					// The card the link sits in is still rendered.
					assert.Contains(t, out, `>Details</h3>`)
					return
				}
				require.Len(t, m, 1)
				assert.Equal(t, tt.wantHref, html.UnescapeString(m[0][1]))
			})
		}
	}
}
