package report

import (
	"time"

	"github.com/google/uuid"
)

type SearchReport struct {
	ID         uuid.UUID  `json:"id" gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SearchID   uuid.UUID  `json:"search_id" gorm:"type:uuid;not null"`
	UserID     uuid.UUID  `json:"user_id" gorm:"type:uuid;not null"`
	Reason     string     `json:"reason"`
	Resolved   bool       `json:"resolved"`
	ResolvedBy *uuid.UUID `json:"resolved_by,omitempty" gorm:"type:uuid"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (SearchReport) TableName() string { return "search_reports" }
