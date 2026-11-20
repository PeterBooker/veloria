package repo

// SearchLimiter bounds how many extension scans run at once.
//
// One limiter is shared by every store, so its size (SEARCH_CONCURRENCY) caps
// scans for the whole process. Before, each Search call had a worker pool of
// that size to itself, and N concurrent searches ran N times as many scans.
type SearchLimiter struct {
	slots chan struct{}
}

// NewSearchLimiter returns a limiter that lets n scans run at once.
// Values below one are treated as one.
func NewSearchLimiter(n int) *SearchLimiter {
	return &SearchLimiter{slots: make(chan struct{}, max(n, 1))}
}
