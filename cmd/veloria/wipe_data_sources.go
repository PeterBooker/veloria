package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// WipeDataSourcesCmd wipes all extension data from the database and disk.
type WipeDataSourcesCmd struct {
	Force bool `help:"Skip confirmation prompt." short:"f"`
}

func (c *WipeDataSourcesCmd) Run() error {
	cfg, err := loadConfigForWipe()
	if err != nil {
		return err
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if !c.Force {
		if !confirmWipe("This will permanently delete ALL plugins, themes, and cores from the database and disk.") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Truncate database tables.
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec("TRUNCATE plugins, themes, cores, largest_repo_files, index_events CASCADE"); err != nil {
		return fmt.Errorf("failed to truncate tables: %w", err)
	}

	if _, err := tx.Exec("UPDATE datasources SET last_full_scan_at = NULL, last_update_at = NULL"); err != nil {
		return fmt.Errorf("failed to reset datasource timestamps: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Println("Wiped database tables.")
	fmt.Println("  - Truncated: plugins, themes, cores, largest_repo_files, index_events")
	fmt.Println("  - Reset: datasource scan timestamps")

	// Remove source and index files from disk, then recreate the empty directories.
	repoTypes := []string{"plugins", "themes", "cores"}
	subDirs := []string{"source", "index"}
	for _, repo := range repoTypes {
		for _, sub := range subDirs {
			dir := filepath.Join(cfg.DataDir, repo, sub)
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("failed to remove %s: %w", dir, err)
			}
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("failed to recreate %s: %w", dir, err)
			}
		}
	}

	fmt.Println("Wiped source and index files from disk.")
	fmt.Println("Wipe complete.")
	return nil
}
