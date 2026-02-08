package index

// GetRegexpPattern wraps a pattern with regex flags.
func GetRegexpPattern(pat string, ignoreCase bool) string {
	if ignoreCase {
		return "(?i)(?m)" + pat
	}
	return "(?m)" + pat
}
