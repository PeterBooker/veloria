package auth

import (
	"time"

	"github.com/google/uuid"
)

// OAuthIdentity represents an OAuth identity linked to a user.
type OAuthIdentity struct {
	ID           uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	UserID       uuid.UUID  `gorm:"type:uuid;not null"`
	Provider     string     `gorm:"size:50;not null"`
	ProviderID   string     `gorm:"size:255;not null"`
	AccessToken  string     `gorm:"size:500"`
	RefreshToken string     `gorm:"size:500"`
	ExpiresAt    *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (OAuthIdentity) TableName() string {
	return "oauth_identities"
}
