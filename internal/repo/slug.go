package repo

import (
	"strconv"
	"unicode"
	"unicode/utf8"
)

// maxSlugLen is the longest plugin or theme slug accepted, in bytes.
const maxSlugLen = 200

// ValidSlug reports whether slug may be used as an extension's identifier.
// The slug names one directory under <datadir>/<type>/source and
// <datadir>/<type>/index, and the indexer derives the sibling names
// <slug>.lock, <slug>.staging and <slug>.old from it, so it must be a single
// plain path component that cannot collide with those.
//
// Plugins and themes: 1 to maxSlugLen bytes of valid UTF-8 in which every
// rune is a Unicode letter or digit, '-', '_' or '%' (wordpress.org
// percent-encodes non-ASCII slugs). No dot is allowed, which also keeps a slug
// out of the legacy <slug>.<timestamp> index names read by LoadIndexes.
//
// Cores: a stable release version, as accepted by FetchWordPressReleaseZips.
func ValidSlug(t ExtensionType, slug string) bool {
	if t == TypeCores {
		return stableVersionRe.MatchString(slug)
	}
	if slug == "" || len(slug) > maxSlugLen || !utf8.ValidString(slug) {
		return false
	}
	for _, r := range slug {
		if r != '-' && r != '_' && r != '%' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// quoteSlug returns slug as a bounded, ASCII-only quoted string so an
// untrusted value can be logged without forging or flooding log output.
func quoteSlug(slug string) string {
	if len(slug) > maxSlugLen {
		return strconv.QuoteToASCII(slug[:maxSlugLen]) + "..."
	}
	return strconv.QuoteToASCII(slug)
}
