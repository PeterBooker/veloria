package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"veloria/internal/user"
)

const (
	tokenPrefix    = "vel_"
	tokenRawLen    = 32 // 256 bits of entropy
	tokenSuffixLen = 8  // last 8 chars stored for display
	maxTokensPerUser = 10
)

// APIToken represents a user-created API token for MCP authentication.
type APIToken struct {
	ID         uuid.UUID  `json:"id" gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	UserID     uuid.UUID  `json:"user_id" gorm:"type:uuid;not null"`
	Name       string     `json:"name" gorm:"size:100;not null"`
	TokenHash  string     `json:"-" gorm:"size:64;not null;uniqueIndex"`
	Suffix     string     `json:"suffix" gorm:"size:8;not null"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (APIToken) TableName() string {
	return "api_tokens"
}

// GenerateToken creates a new random API token string with the "vel_" prefix.
// Returns the raw token (to show the user once) and its SHA-256 hex hash.
func GenerateToken() (raw, hash string, err error) {
	b := make([]byte, tokenRawLen)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	raw = tokenPrefix + hex.EncodeToString(b)
	hash = hashToken(raw)
	return raw, hash, nil
}

// hashToken returns the SHA-256 hex digest of a raw token string.
func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// tokenSuffix returns the last tokenSuffixLen characters of a raw token for display.
func tokenSuffix(raw string) string {
	if len(raw) <= tokenSuffixLen {
		return raw
	}
	return raw[len(raw)-tokenSuffixLen:]
}

// CreateToken generates a new API token for the given user.
// Returns the raw token (shown once) and the persisted record.
func CreateToken(ctx context.Context, db *gorm.DB, userID uuid.UUID, name string) (string, *APIToken, error) {
	// Enforce per-user limit.
	var count int64
	if err := db.WithContext(ctx).Model(&APIToken{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return "", nil, fmt.Errorf("failed to check token count: %w", err)
	}
	if count >= maxTokensPerUser {
		return "", nil, fmt.Errorf("maximum of %d API tokens per user", maxTokensPerUser)
	}

	raw, hash, err := GenerateToken()
	if err != nil {
		return "", nil, err
	}

	token := &APIToken{
		UserID:    userID,
		Name:      name,
		TokenHash: hash,
		Suffix:    tokenSuffix(raw),
	}
	if err := db.WithContext(ctx).Create(token).Error; err != nil {
		return "", nil, fmt.Errorf("failed to create token: %w", err)
	}

	return raw, token, nil
}

// ListTokens returns all API tokens for a user (without hashes).
func ListTokens(ctx context.Context, db *gorm.DB, userID uuid.UUID) ([]APIToken, error) {
	var tokens []APIToken
	err := db.WithContext(ctx).
		Select("id, user_id, name, suffix, last_used_at, expires_at, created_at").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&tokens).Error
	return tokens, err
}

// DeleteToken removes an API token, scoped to the owning user.
func DeleteToken(ctx context.Context, db *gorm.DB, userID, tokenID uuid.UUID) error {
	result := db.WithContext(ctx).Where("id = ? AND user_id = ?", tokenID, userID).Delete(&APIToken{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// ValidateToken checks a raw Bearer token and returns the owning user.
// Returns nil, nil if the token is invalid or expired.
func ValidateToken(ctx context.Context, db *gorm.DB, rawToken string) (*user.User, error) {
	hash := hashToken(rawToken)

	var token APIToken
	err := db.WithContext(ctx).Where("token_hash = ?", hash).First(&token).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to look up token: %w", err)
	}

	// Check expiry.
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil, nil
	}

	// Load the owning user.
	var u user.User
	if err := db.WithContext(ctx).First(&u, "id = ? AND deleted_at IS NULL", token.UserID).Error; err != nil {
		return nil, nil
	}

	// Update last_used_at asynchronously to avoid adding latency.
	go func() {
		_ = db.Model(&token).Update("last_used_at", time.Now()).Error
	}()

	return &u, nil
}
