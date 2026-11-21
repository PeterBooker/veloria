package repo

import (
	"strings"
	"testing"
)

func TestValidSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		slug string
		want bool
	}{
		{"plain", "akismet", true},
		{"hyphens", "wp-super-cache", true},
		{"underscore", "my_plugin", true},
		{"digits", "2fa", true},
		{"uppercase", "WooCommerce", true},
		{"non-ASCII", "日本語-プラグイン", true},
		{"percent-encoded", "%e6%97%a5%e6%9c%ac%e8%aa%9e", true},
		{"200 bytes", strings.Repeat("a", 200), true},

		{"empty", "", false},
		{"dot", ".", false},
		{"dot dot", "..", false},
		{"slash", "a/b", false},
		{"backslash", `a\b`, false},
		{"parent traversal", "../akismet", false},
		{"legacy timestamp suffix", "x.1700000000", false},
		{"staging suffix", "x.staging", false},
		{"old suffix", "x.old", false},
		{"lock suffix", "x.lock", false},
		{"leading dot", ".hidden", false},
		{"version-like", "plugin.1.0", false},
		{"newline", "a\nb", false},
		{"trailing newline", "akismet\n", false},
		{"single quote", "it's", false},
		{"double quote", `a"b`, false},
		{"space", "a b", false},
		{"tab", "a\tb", false},
		{"NUL", "a\x00b", false},
		{"angle bracket", "a<b", false},
		{"invalid UTF-8", "a\xffb", false},
		{"201 bytes", strings.Repeat("a", 201), false},
		{"201 bytes non-ASCII", strings.Repeat("é", 100) + "a", false},
	}

	for _, repoType := range []ExtensionType{TypePlugins, TypeThemes} {
		for _, tt := range tests {
			if got := ValidSlug(repoType, tt.slug); got != tt.want {
				t.Errorf("ValidSlug(%s, %q) [%s] = %v, want %v", repoType, tt.slug, tt.name, got, tt.want)
			}
		}
	}
}

func TestValidSlugCores(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slug string
		want bool
	}{
		{"6.8", true},
		{"6.8.1", true},
		{"3.5", true},
		{"10.0.12", true},

		{"", false},
		{".", false},
		{"..", false},
		{"6", false},
		{"6.", false},
		{".8", false},
		{"6..8", false},
		{"6.8.1.2", false},
		{"6.8-RC1", false},
		{"6.8/..", false},
		{`6.8\1`, false},
		{"6.8\n", false},
		{"6.8'", false},
		{"akismet", false},
	}

	for _, tt := range tests {
		if got := ValidSlug(TypeCores, tt.slug); got != tt.want {
			t.Errorf("ValidSlug(cores, %q) = %v, want %v", tt.slug, got, tt.want)
		}
	}
}

func TestQuoteSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slug string
		want string
	}{
		{"akismet", `"akismet"`},
		{"a\nb", `"a\nb"`},
		{"it's", `"it's"`},
		{"a\x1b[31mb", `"a\x1b[31mb"`},
		{"\u202egnp", `"\u202egnp"`},
		{"日本", `"\u65e5\u672c"`},
		{strings.Repeat("a", maxSlugLen+50), `"` + strings.Repeat("a", maxSlugLen) + `"...`},
	}

	for _, tt := range tests {
		if got := quoteSlug(tt.slug); got != tt.want {
			t.Errorf("quoteSlug(%q) = %s, want %s", tt.slug, got, tt.want)
		}
	}
}
