package index

import (
	"fmt"
	"regexp/syntax"
	"slices"
)

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
