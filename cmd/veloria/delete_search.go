package main

import (
	"context"
	"fmt"

	"veloria/internal/storage"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DeleteSearchCmd deletes a single search by ID from the database and S3 storage.
type DeleteSearchCmd struct {
	ID    string `arg:"" help:"Search UUID to delete."`
	Force bool   `help:"Skip confirmation prompt." short:"f"`
}

func (c *DeleteSearchCmd) Run() error {
	searchID, err := uuid.Parse(c.ID)
	if err != nil {
		return fmt.Errorf("invalid search ID %q: %w", c.ID, err)
	}

	cfg, err := loadConfigForWipe()
	if err != nil {
		return err
	}

	db, err := openDB(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Verify the search exists.
	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM searches WHERE id = $1)", searchID).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check search existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("search %s not found", searchID)
	}

	if !c.Force {
		if !confirmWipe(fmt.Sprintf("This will permanently delete search %s.", searchID)) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	s3, err := storage.NewS3Client(cfg, zap.NewNop())
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	ctx := context.Background()
	if err := s3.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("failed to verify S3 bucket: %w", err)
	}

	// Delete S3 object (ignore not-found errors).
	if err := s3.DeleteResult(ctx, searchID.String()); err != nil {
		return fmt.Errorf("failed to delete S3 object: %w", err)
	}

	// Delete from database.
	result, err := db.Exec("DELETE FROM searches WHERE id = $1", searchID)
	if err != nil {
		return fmt.Errorf("failed to delete search from database: %w", err)
	}

	rows, _ := result.RowsAffected()
	fmt.Printf("Deleted search %s (%d row removed).\n", searchID, rows)
	return nil
}
