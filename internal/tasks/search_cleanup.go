package tasks

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	searchmodel "veloria/internal/search/model"
)

const (
	// StuckSearchTimeout is how long a search can be in processing state before being marked as failed
	StuckSearchTimeout = 60 * time.Second
	// SearchCleanupInterval is how often to check for stuck searches
	SearchCleanupInterval = 30 * time.Second
)

// CleanupStuckSearches marks searches that have been processing for too long as failed
func CleanupStuckSearches(db *gorm.DB, l *zerolog.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		cutoff := time.Now().Add(-StuckSearchTimeout)

		result := db.WithContext(ctx).
			Model(&searchmodel.Search{}).
			Where("status = ? AND updated_at < ?", searchmodel.StatusProcessing, cutoff).
			Update("status", searchmodel.StatusFailed)

		if result.Error != nil {
			l.Error().Err(result.Error).Msg("Failed to cleanup stuck searches")
			return result.Error
		}

		if result.RowsAffected > 0 {
			l.Info().Int64("count", result.RowsAffected).Msg("Marked stuck searches as failed")
		}

		return nil
	}
}
