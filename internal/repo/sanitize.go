package repo

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxVarcharLen is the width of the VARCHAR(255) columns on the plugins and
// themes tables (name, version, requires, tested, requires_php). Postgres
// enforces VARCHAR(n) in characters on a UTF8 database, so every length check
// here counts runes rather than bytes.
const maxVarcharLen = 255

// sanitizeText makes an upstream string safe for a VARCHAR(limit) column using
// the least destructive change that fits:
//
//  1. Always drop control characters, collapse whitespace runs to one space
//     and trim. These never carry meaning in a metadata field.
//  2. If still over the limit, also drop Unicode format characters (zero-width
//     spaces and joiners, BOM, bidi marks). Short values keep them because
//     ZWNJ and ZWJ are orthographically required in Persian, Urdu and several
//     Indic scripts and glue emoji sequences together.
//  3. If still over the limit, truncate to limit runes without splitting a
//     combining-mark sequence or an HTML entity (names arrive HTML-escaped and
//     are unescaped for display by GetName).
//
// A limit of zero disables steps 2 and 3.
func sanitizeText(s string, limit int) string {
	if s == "" {
		return s
	}
	out := cleanText(s, false)
	if limit <= 0 || utf8.RuneCountInString(out) <= limit {
		return out
	}
	out = cleanText(s, true)
	if utf8.RuneCountInString(out) <= limit {
		return out
	}
	return truncateRunes(out, limit)
}

// cleanText drops control characters, collapses whitespace and trims. With
// dropFormat set it also drops Unicode format (Cf) characters.
func cleanText(s string, dropFormat bool) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			pendingSpace = b.Len() > 0
		case unicode.IsControl(r), dropFormat && unicode.Is(unicode.Cf, r):
			// Dropped.
		default:
			if pendingSpace {
				b.WriteByte(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncateRunes cuts s to at most limit runes. The cut backs up so that a
// base character is not separated from its combining marks and an HTML
// entity such as "&amp;" is not left half-written, then trailing space is
// trimmed.
func truncateRunes(s string, limit int) string {
	i := 0
	for n := 0; n < limit; n++ {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
	}
	for i > 0 {
		r, _ := utf8.DecodeRuneInString(s[i:])
		if !unicode.Is(unicode.M, r) {
			break
		}
		_, w := utf8.DecodeLastRuneInString(s[:i])
		i -= w
	}
	head := s[:i]
	if amp := strings.LastIndexByte(head, '&'); amp >= 0 && !strings.ContainsAny(head[amp:], "; ") {
		head = head[:amp]
	}
	return strings.TrimSpace(head)
}

// normalizeName sanitizes a display name. wordpress.org derives the name from
// the "=== Name ===" readme header; when that line carries invisible
// characters the parser falls through and the markers leak into the published
// value (dreamanual-toolkit arrived as 3,584 zero-width characters followed by
// "=== Dreamanual Toolkit"). No real name starts with "===", so the prefix is
// a reliable signature of that leak. A name with no visible character at all
// normalizes to "" so the caller can substitute the slug.
func normalizeName(s string) string {
	s = sanitizeText(s, maxVarcharLen)
	if cleanText(s, true) == "" {
		return ""
	}
	if strings.HasPrefix(s, "===") {
		s = strings.TrimSpace(strings.Trim(s, "="))
	}
	return s
}

// normalize bounds every field backed by a VARCHAR(255) column so that
// upstream metadata cannot fail the row insert. It runs from UnmarshalJSON,
// which every record decoded from the API passes through regardless of the
// fetch path (update feed, discovery scan, single-item lookup).
func (p *Plugin) normalize() {
	p.Name = normalizeName(p.Name)
	if p.Name == "" {
		p.Name = p.Slug
	}
	p.Version = sanitizeText(p.Version, maxVarcharLen)
	p.Requires = sanitizeText(p.Requires, maxVarcharLen)
	p.Tested = sanitizeText(p.Tested, maxVarcharLen)
	p.RequiresPHP = sanitizeText(p.RequiresPHP, maxVarcharLen)
}

// normalize is the Theme counterpart of Plugin.normalize.
func (t *Theme) normalize() {
	t.Name = normalizeName(t.Name)
	if t.Name == "" {
		t.Name = t.Slug
	}
	t.Version = sanitizeText(t.Version, maxVarcharLen)
	t.Requires = sanitizeText(t.Requires, maxVarcharLen)
	t.Tested = sanitizeText(t.Tested, maxVarcharLen)
	t.RequiresPHP = sanitizeText(t.RequiresPHP, maxVarcharLen)
}
