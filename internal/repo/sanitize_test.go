package repo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	zwsp = "\u200b" // ZERO WIDTH SPACE
	zwnj = "\u200c" // ZERO WIDTH NON-JOINER
	zwj  = "\u200d" // ZERO WIDTH JOINER
	bom  = "\ufeff" // BYTE ORDER MARK
	nbsp = "\u00a0" // NO-BREAK SPACE
)

// zeroWidthPad returns n alternating U+200B / U+200D characters.
func zeroWidthPad(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			b.WriteString(zwsp)
		} else {
			b.WriteString(zwj)
		}
	}
	return b.String()
}

// productionName mirrors the dreamanual-toolkit payload observed on
// 2026-09-01: one block of 3,584 zero-width characters followed by the
// readme header text.
func productionName() string {
	return zeroWidthPad(3584) + "=== Dreamanual Toolkit"
}

func TestSanitizeText(t *testing.T) {
	persian := "می" + zwnj + "خواهم افزونه"
	family := "Family 👨" + zwj + "👩" + zwj + "👧 Plugin"

	tests := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{name: "plain ascii unchanged", in: "Dreamanual Toolkit", limit: 255, want: "Dreamanual Toolkit"},
		{name: "empty", in: "", limit: 255, want: ""},
		{name: "control chars removed", in: "Dream\x00anual\x07 Toolkit", limit: 255, want: "Dreamanual Toolkit"},
		{name: "whitespace collapsed and trimmed", in: "  Dreamanual \t\n" + nbsp + " Toolkit \r\n", limit: 255, want: "Dreamanual Toolkit"},
		{name: "unicode letters kept", in: "Ünïcödé 日本語 Plugin", limit: 255, want: "Ünïcödé 日本語 Plugin"},
		{name: "short value keeps ZWNJ", in: persian, limit: 255, want: persian},
		{name: "short value keeps ZWJ emoji sequence", in: family, limit: 255, want: family},
		{name: "short value keeps a stray zero-width space", in: "Tool" + zwsp + "kit", limit: 255, want: "Tool" + zwsp + "kit"},
		{name: "exactly at the limit is untouched", in: strings.Repeat("é", 255), limit: 255, want: strings.Repeat("é", 255)},
		{name: "over the limit drops format chars first", in: zeroWidthPad(3584) + bom + "Toolkit", limit: 255, want: "Toolkit"},
		{name: "over the limit drops ZWJ before truncating", in: strings.Repeat(zwj, 300) + "Family", limit: 255, want: "Family"},
		{name: "truncates by rune count when stripping is not enough", in: strings.Repeat("é", 300), limit: 255, want: strings.Repeat("é", 255)},
		{name: "truncation drops trailing space", in: strings.Repeat("ab ", 100), limit: 3, want: "ab"},
		{name: "truncation keeps a base with its combining marks", in: strings.Repeat("a", 254) + "e\u0301x", limit: 255, want: strings.Repeat("a", 254)},
		{name: "truncation does not leave a broken entity", in: strings.Repeat("a", 253) + "&amp; more", limit: 255, want: strings.Repeat("a", 253)},
		{name: "truncation keeps a bare ampersand followed by a space", in: strings.Repeat("a", 252) + " & more", limit: 255, want: strings.Repeat("a", 252) + " &"},
		{name: "limit zero disables truncation", in: strings.Repeat("x", 300), limit: 0, want: strings.Repeat("x", 300)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeText(tc.in, tc.limit)
			assert.Equal(t, tc.want, got)
			if tc.limit > 0 {
				assert.LessOrEqual(t, utf8.RuneCountInString(got), tc.limit)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "readme markers both sides", in: "=== Dreamanual Toolkit ===", want: "Dreamanual Toolkit"},
		{name: "readme marker leading only", in: "=== Dreamanual Toolkit", want: "Dreamanual Toolkit"},
		{name: "longer marker runs", in: "==== Dreamanual Toolkit ====", want: "Dreamanual Toolkit"},
		{name: "no markers unchanged", in: "Akismet Anti-spam", want: "Akismet Anti-spam"},
		{name: "equals inside name kept", in: "A = B Calculator", want: "A = B Calculator"},
		{name: "trailing equals without leading marker kept", in: "Redirect =", want: "Redirect ="},
		{name: "production payload", in: productionName(), want: "Dreamanual Toolkit"},
		{name: "overlong visible name truncated", in: strings.Repeat("Long Name ", 60), want: strings.TrimSpace(strings.Repeat("Long Name ", 60)[:255])},
		{name: "only invisible characters becomes empty", in: zeroWidthPad(40), want: ""},
		{name: "only markers becomes empty", in: "===", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeName(tc.in)
			assert.Equal(t, tc.want, got)
			assert.LessOrEqual(t, utf8.RuneCountInString(got), maxVarcharLen)
		})
	}
}

// apiPayload builds an API-shaped JSON document for one plugin or theme.
// The two types share the fields that normalization touches, so the same
// document exercises both decoders.
func apiPayload(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	doc := map[string]any{
		"name":          productionName(),
		"slug":          "dreamanual-toolkit",
		"version":       " 1.4.0 ",
		"requires":      "6.4",
		"tested":        strings.Repeat("7.1 ", 100),
		"requires_php":  7.4, // sometimes a number upstream; goes through toString first
		"download_link": "https://example.com/dreamanual-toolkit.1.4.0.zip",
		"last_updated":  "2026-08-25 2:16am GMT",
	}
	for k, v := range overrides {
		doc[k] = v
	}
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	return data
}

func TestPluginUnmarshalJSONNormalizes(t *testing.T) {
	var p Plugin
	require.NoError(t, json.Unmarshal(apiPayload(t, nil), &p))

	assert.Equal(t, "Dreamanual Toolkit", p.Name)
	assert.Equal(t, "1.4.0", p.Version, "whitespace must not leak into download URLs built from the version")
	assert.Equal(t, "6.4", p.Requires)
	assert.Equal(t, "7.4", p.RequiresPHP)
	assert.LessOrEqual(t, utf8.RuneCountInString(p.Tested), maxVarcharLen)
	assert.Equal(t, "dreamanual-toolkit", p.Slug, "slug is an identifier and must not be rewritten")
}

func TestPluginUnmarshalJSONFallsBackToSlugForBlankName(t *testing.T) {
	var p Plugin
	require.NoError(t, json.Unmarshal(apiPayload(t, map[string]any{"name": zeroWidthPad(3000)}), &p))
	assert.Equal(t, "dreamanual-toolkit", p.Name)
}

func TestPluginUnmarshalJSONKeepsShortJoinedNames(t *testing.T) {
	persian := "می" + zwnj + "خواهم افزونه"
	var p Plugin
	require.NoError(t, json.Unmarshal(apiPayload(t, map[string]any{"name": persian}), &p))
	assert.Equal(t, persian, p.Name)
}

func TestThemeUnmarshalJSONNormalizes(t *testing.T) {
	var th Theme
	require.NoError(t, json.Unmarshal(apiPayload(t, nil), &th))

	assert.Equal(t, "Dreamanual Toolkit", th.Name)
	assert.Equal(t, "1.4.0", th.Version)
	assert.Equal(t, "7.4", th.RequiresPHP)
	assert.LessOrEqual(t, utf8.RuneCountInString(th.Tested), maxVarcharLen)
	assert.Equal(t, "dreamanual-toolkit", th.Slug)

	var blank Theme
	require.NoError(t, json.Unmarshal(apiPayload(t, map[string]any{"name": "==="}), &blank))
	assert.Equal(t, "dreamanual-toolkit", blank.Name)
}

// The update feed and discovery scan both go through fetchPluginPage, so a
// record served by the API must already be bounded when it reaches PrepareUpdates.
func TestFetchPluginPageNormalizes(t *testing.T) {
	body := []byte(`{"info":{"page":1,"pages":1,"results":1},"plugins":[` + string(apiPayload(t, nil)) + `]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	api := newTestAPIClient(t, ThrottleConfig{MaxRetries: 1, DefaultRetryDelay: 10 * time.Millisecond})
	plugins, info, err := fetchPluginPage(context.Background(), api, srv.URL)
	require.NoError(t, err)
	require.Len(t, plugins, 1)
	assert.Equal(t, 1, info.Results)
	assert.Equal(t, "Dreamanual Toolkit", plugins[0].Name)
	assert.LessOrEqual(t, utf8.RuneCountInString(plugins[0].Tested), maxVarcharLen)
}
