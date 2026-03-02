package repo

import (
	"database/sql"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// readWatermark reads the last_update_at timestamp from the datasources table
// for the given repo type. Falls back to 1 hour ago if no watermark is stored.
func readWatermark(db *gorm.DB, repoType ExtensionType) time.Time {
	var lastUpdate sql.NullTime
	err := db.Table("datasources").
		Where("repo_type = ?", string(repoType)).
		Pluck("last_update_at", &lastUpdate).Error
	if err != nil || !lastUpdate.Valid {
		return time.Now().UTC().Add(-1 * time.Hour)
	}
	return lastUpdate.Time
}

// writeWatermark updates the last_update_at timestamp in the datasources table.
func writeWatermark(db *gorm.DB, repoType ExtensionType, l *zap.Logger) {
	err := db.Table("datasources").
		Where("repo_type = ?", string(repoType)).
		Update("last_update_at", time.Now().UTC()).Error
	if err != nil {
		l.Error("Failed to write update watermark", zap.String("type", string(repoType)), zap.Error(err))
	}
}
