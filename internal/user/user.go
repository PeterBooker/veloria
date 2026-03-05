package user

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `json:"id" gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	PasswordHash *string    `json:"-" gorm:"column:password_hash"`
	AvatarURL    *string    `json:"avatar_url,omitempty" gorm:"column:avatar_url"`
	IsAdmin      bool       `json:"is_admin"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}
