package index

import "regexp/syntax"

// getRegexpPattern wraps a pattern with regex flags.
func getRegexpPattern(pat string, ignoreCase bool) string {
	if ignoreCase {
		return "(?i)(?m)" + pat
	}
	return "(?m)" + pat
}

// ValidatePattern checks whether pat is a valid regular expression.
func ValidatePattern(pat string) error {
	_, err := syntax.Parse("(?m)"+pat, syntax.Perl)
	return err
}
