package main

import (
	"context"
	"fmt"
)

// StatsCmd prints index statistics for each repository type.
type StatsCmd struct{}

func (c *StatsCmd) Run() error {
	cfg, err := loadConfigForWipe()
	if err != nil {
		return err
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()

	fmt.Printf("%-10s %8s %8s %8s\n", "REPO", "TOTAL", "INDEXED", "COVERAGE")
	fmt.Printf("%-10s %8s %8s %8s\n", "----", "-----", "-------", "--------")

	tables := []struct {
		name  string
		table string
	}{
		{"plugins", "plugins"},
		{"themes", "themes"},
		{"cores", "cores"},
	}

	for _, t := range tables {
		var total, indexed int
		err := db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT COUNT(*), COUNT(*) FILTER (WHERE index_status = 'indexed') FROM %s", t.table),
		).Scan(&total, &indexed)
		if err != nil {
			return fmt.Errorf("failed to query %s stats: %w", t.name, err)
		}

		var pct float64
		if total > 0 {
			pct = float64(indexed) / float64(total) * 100
		}
		fmt.Printf("%-10s %8d %8d %7.1f%%\n", t.name, total, indexed, pct)
	}

	return nil
}
