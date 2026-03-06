package search

import "testing"

func TestShouldForcePrivate(t *testing.T) {
	tests := []struct {
		name string
		term string
		want bool
	}{
		// Normal search terms — should remain public.
		{"normal term", "add_action", false},
		{"normal with spaces", "wp_query meta_key", false},
		{"regex pattern", `\.php$`, false},
		{"code pattern", "eval(base64_decode", false},

		// URLs — should be forced private.
		{"http url", "https://example.com/scam", true},
		{"http no s", "http://spam.net", true},
		{"www prefix", "www.buycheapstuff.com", true},
		{"bare domain .com", "visit example.com today", true},
		{"bare domain .net", "check spam.net now", true},
		{"bare domain .org", "go to evil.org", true},
		{"bare domain .io", "my-site.io", true},
		{"bare domain .xyz", "cheap.xyz", true},
		{"bare domain .ru", "malware.ru", true},
		{"bare domain .shop", "deals.shop", true},

		// Blocked words — should be forced private.
		{"slur", "nigger", true},
		{"slur mixed case", "NIGGER", true},
		{"slur in phrase", "some nigga stuff", true},
		{"offensive word", "faggot", true},
		{"spam keyword", "buy cheap pills", true},
		{"porn site", "pornhub", true},
		{"casino spam", "casino bonus free", true},
		{"viagra spam", "viagra online", true},

		// Already private — function still returns true (caller handles dedup).
		{"url still flagged", "https://scam.com", true},

		// Edge cases.
		{"empty string", "", false},
		{"domain-like but not TLD", "example.notarealtld", false},
		{"partial word ok", "document", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldForcePrivate(tt.term); got != tt.want {
				t.Errorf("shouldForcePrivate(%q) = %v, want %v", tt.term, got, tt.want)
			}
		})
	}
}
