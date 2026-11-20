package cache

// NewRistrettoWithMaxCost creates a cache with a small byte budget for tests.
func NewRistrettoWithMaxCost(maxCost int64) (*RistrettoCache, error) {
	return newRistretto(maxCost)
}

// Wait blocks until every pending Set has been applied.
func (r *RistrettoCache) Wait() {
	r.c.Wait()
}

// UsedCost returns the cost currently charged against the budget.
func (r *RistrettoCache) UsedCost() int64 {
	return r.c.MaxCost() - r.c.RemainingCost()
}
