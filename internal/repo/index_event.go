package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// IndexEventStatus represents the outcome of an indexing task.
type IndexEventStatus string

const (
	IndexEventSuccess IndexEventStatus = "success"
	IndexEventFailed  IndexEventStatus = "failed"
	IndexEventSkipped IndexEventStatus = "skipped"
)

// IndexEvent records the outcome of a single indexing attempt.
type IndexEvent struct {
	ID           uuid.UUID        `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	RepoType     string           `gorm:"column:repo_type;size:20;not null"`
	Slug         string           `gorm:"size:255;not null"`
	Status       IndexEventStatus `gorm:"size:20;not null"`
	ErrorMessage string           `gorm:"type:text"`
	DurationMS   int64            `gorm:"column:duration_ms;not null;default:0"`
	CreatedAt    time.Time        `gorm:"not null;default:now()"`
}

func (IndexEvent) TableName() string { return "index_events" }

// IndexEventRecorder persists indexing outcomes to the database.
type IndexEventRecorder struct {
	db *gorm.DB
	l  *zap.Logger
}

// NewIndexEventRecorder creates a new recorder.
func NewIndexEventRecorder(db *gorm.DB, l *zap.Logger) *IndexEventRecorder {
	return &IndexEventRecorder{db: db, l: l}
}

// Record saves an indexing event. Errors are logged, not returned, to avoid
// blocking the indexing pipeline.
func (r *IndexEventRecorder) Record(event IndexEvent) {
	if err := r.db.Create(&event).Error; err != nil {
		r.l.Error("Failed to record index event",
			zap.String("repo_type", event.RepoType),
			zap.String("slug", event.Slug),
			zap.Error(err),
		)
	}
}

// RecentFailures returns the most recent failed events since the given time.
func (r *IndexEventRecorder) RecentFailures(since time.Time, limit int) ([]IndexEvent, error) {
	var events []IndexEvent
	err := r.db.Where("status = ? AND created_at >= ?", IndexEventFailed, since).
		Order("created_at DESC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

// RecentEvents returns recent index events across all statuses.
func (r *IndexEventRecorder) RecentEvents(limit int) ([]IndexEvent, error) {
	var events []IndexEvent
	err := r.db.Order("created_at DESC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

// ClearFailures removes failed events for a specific slug+repo after a
// successful re-index, so the failures table stays consistent with reality.
func (r *IndexEventRecorder) ClearFailures(repoType, slug string) {
	result := r.db.Where("repo_type = ? AND slug = ? AND status = ?", repoType, slug, IndexEventFailed).
		Delete(&IndexEvent{})
	if result.Error != nil {
		r.l.Warn("Failed to clear old failure events",
			zap.String("repo_type", repoType),
			zap.String("slug", slug),
			zap.Error(result.Error),
		)
	}
}

// CleanupOldEvents removes index events older than the given duration.
// Intended to be called as a periodic background task.
func CleanupOldEvents(db *gorm.DB, l *zap.Logger, maxAge time.Duration) func(context.Context) error {
	return func(_ context.Context) error {
		cutoff := time.Now().Add(-maxAge)
		result := db.Where("created_at < ?", cutoff).Delete(&IndexEvent{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			l.Info("Cleaned up old index events", zap.Int64("deleted", result.RowsAffected))
		}
		return nil
	}
}
