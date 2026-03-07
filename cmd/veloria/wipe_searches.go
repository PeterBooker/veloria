package main

import (
	"context"
	"fmt"

	"veloria/internal/storage"

	"go.uber.org/zap"
)

// WipeSearchesCmd wipes all searches from the database and S3 storage.
type WipeSearchesCmd struct {
	Force bool `help:"Skip confirmation prompt." short:"f"`
}

func (c *WipeSearchesCmd) Run() error {
	cfg, err := loadConfigForWipe()
	if err != nil {
		return err
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	s3, err := storage.NewS3Client(cfg, zap.NewNop())
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	ctx := context.Background()
	if err := s3.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("failed to verify S3 bucket: %w", err)
	}

	if !c.Force {
		if !confirmWipe("This will permanently delete ALL searches from the database and S3 storage.") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Delete all search result objects from S3.
	fmt.Print("Deleting S3 objects... ")
	deleted, err := s3.DeleteAllResults(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete S3 objects: %w", err)
	}
	fmt.Printf("%d objects deleted.\n", deleted)

	// Truncate database tables.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec("TRUNCATE searches, search_reports CASCADE"); err != nil {
		return fmt.Errorf("failed to truncate tables: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Println("Wiped all searches successfully.")
	fmt.Println("  - Truncated: searches, search_reports")
	return nil
}
