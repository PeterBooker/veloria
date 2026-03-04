package index

// getRegexpPattern wraps a pattern with regex flags.
func getRegexpPattern(pat string, ignoreCase bool) string {
	if ignoreCase {
		return "(?i)(?m)" + pat
	}
	return "(?m)" + pat
}
