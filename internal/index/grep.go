package index

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
)

const (
	// MaxPatternLen is the longest search term accepted, in bytes. It matches
	// the width of the searches.term column.
	MaxPatternLen = 255

	// MaxFileFilterLen is the longest file-name filter regex accepted, in
	// bytes. A filter runs against the path of every candidate file, so its
	// size bounds the cost of each of those matches.
	MaxFileFilterLen = 255
)

var (
	// ErrPatternTooLong is returned for a search term over MaxPatternLen.
	ErrPatternTooLong = fmt.Errorf("search term is longer than %d bytes", MaxPatternLen)

	// ErrFileFilterTooLong is returned for a file filter over MaxFileFilterLen.
	ErrFileFilterTooLong = fmt.Errorf("file filter is longer than %d bytes", MaxFileFilterLen)
)

// ValidateFileFilter checks a file-name filter (file_match or
// exclude_file_match) when a search is requested: it must fit in
// MaxFileFilterLen and compile. An empty filter is valid and means no filter.
func ValidateFileFilter(pat string) error {
	if len(pat) > MaxFileFilterLen {
		return ErrFileFilterTooLong
	}
	if pat == "" {
		return nil
	}
	_, err := regexp.Compile(pat)
	return err
}

// getRegexpPattern wraps a pattern with regex flags.
func getRegexpPattern(pat string, ignoreCase bool) string {
	if ignoreCase {
		return "(?i)(?m)" + pat
	}
	return "(?m)" + pat
}

// ValidatePattern checks whether pat is a valid regular expression
// and rejects patterns with empty capture groups like exec() which
// are almost always user mistakes (meant as literal parentheses).
func ValidatePattern(pat string) error {
	re, err := syntax.Parse("(?m)"+pat, syntax.Perl)
	if err != nil {
		return err
	}
	if hasEmptyCapture(re) {
		return fmt.Errorf("use \\( and \\) for literal parentheses")
	}
	return nil
}

// hasEmptyCapture walks the syntax tree and returns true if any
// capture group is empty (matches only the empty string).
func hasEmptyCapture(re *syntax.Regexp) bool {
	if re.Op == syntax.OpCapture {
		if len(re.Sub) == 0 {
			return true
		}
		// A capture with a single empty-match child, e.g. ()
		if len(re.Sub) == 1 && re.Sub[0].Op == syntax.OpEmptyMatch {
			return true
		}
	}
	return slices.ContainsFunc(re.Sub, hasEmptyCapture)
}
