package search

import (
	"regexp"
	"strings"
)

// urlPattern matches common URL patterns in search terms.
var urlPattern = regexp.MustCompile(`(?i)(https?://|www\.|[a-z0-9-]+\.(com|net|org|io|co|info|biz|xyz|ru|cn|tk|ml|ga|cf|gq|top|work|click|link|site|online|store|shop|dev|app)\b)`)

// blockedWords is a fixed list of slurs, insults, and offensive terms that
// should force a search to be private so they don't appear on public listings.
// This is intentionally a curated list of unambiguous slurs — words that have
// no legitimate use in a code-search context.
// If this is not enough use LDNOOBW (List of Dirty, Naughty, Obscene, and Otherwise Bad Words).
var blockedWords = []string{
	"nigger",
	"nigga",
	"faggot",
	"fag",
	"retard",
	"retarded",
	"tranny",
	"kike",
	"spic",
	"wetback",
	"chink",
	"gook",
	"coon",
	"darkie",
	"beaner",
	"towelhead",
	"raghead",
	"whore",
	"slut",
	"cunt",
	"porn",
	"pornhub",
	"xvideos",
	"xhamster",
	"hentai",
	"onlyfans",
	"casino",
	"viagra",
	"cialis",
	"buy cheap",
	"free money",
}

// shouldForcePrivate returns true if the search term contains a URL or
// blocked word, meaning it should not appear in public search listings.
func shouldForcePrivate(term string) bool {
	if urlPattern.MatchString(term) {
		return true
	}

	lower := strings.ToLower(term)
	for _, word := range blockedWords {
		if strings.Contains(lower, word) {
			return true
		}
	}

	return false
}
