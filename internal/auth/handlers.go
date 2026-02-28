package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"veloria/internal/user"
)

// Handler handles OAuth authentication routes.
type Handler struct {
	db           *gorm.DB
	sessionStore *SessionStore
	log          *zap.Logger
}

// NewHandler creates a new auth handler.
func NewHandler(db *gorm.DB, sessionStore *SessionStore, log *zap.Logger) *Handler {
	return &Handler{
		db:           db,
		sessionStore: sessionStore,
		log:          log,
	}
}

// BeginAuth starts the OAuth flow for a provider.
func (h *Handler) BeginAuth(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	r = r.WithContext(context.WithValue(r.Context(), gothic.ProviderParamKey, provider))
	gothic.BeginAuthHandler(w, r)
}

// Callback handles the OAuth callback from the provider.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	r = r.WithContext(context.WithValue(r.Context(), gothic.ProviderParamKey, provider))

	gothUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		h.log.Error("OAuth authentication failed", zap.Error(err), zap.String("provider", provider))
		http.Redirect(w, r, "/login?error=auth_failed", http.StatusSeeOther)
		return
	}

	// Find or create user
	u, err := h.findOrCreateUser(gothUser)
	if err != nil {
		h.log.Error("Failed to process user", zap.Error(err), zap.String("provider", provider))
		http.Redirect(w, r, "/login?error=user_failed", http.StatusSeeOther)
		return
	}

	// Create session
	if err := h.sessionStore.SetUser(w, r, u.ID); err != nil {
		h.log.Error("Failed to create session", zap.Error(err))
		http.Redirect(w, r, "/login?error=session_failed", http.StatusSeeOther)
		return
	}

	h.log.Info("User logged in", zap.String("user_id", u.ID.String()), zap.String("provider", provider))

	// Redirect to home
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout clears the session and redirects to home.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.sessionStore.ClearSession(w, r); err != nil {
		h.log.Error("Failed to clear session", zap.Error(err))
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) findOrCreateUser(gothUser goth.User) (*user.User, error) {
	// Check if OAuth identity exists
	var identity OAuthIdentity
	err := h.db.Where("provider = ? AND provider_id = ?",
		gothUser.Provider, gothUser.UserID).First(&identity).Error

	if err == nil {
		// Identity exists, return associated user
		var u user.User
		if err := h.db.First(&u, "id = ? AND deleted_at IS NULL", identity.UserID).Error; err != nil {
			return nil, err
		}
		// Update tokens
		h.updateOAuthTokens(&identity, gothUser)
		return &u, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Check if user exists with same email
	var existingUser user.User
	err = h.db.Where("email = ? AND deleted_at IS NULL", gothUser.Email).First(&existingUser).Error

	if err == nil {
		// Link OAuth identity to existing user
		identity = OAuthIdentity{
			UserID:       existingUser.ID,
			Provider:     gothUser.Provider,
			ProviderID:   gothUser.UserID,
			AccessToken:  gothUser.AccessToken,
			RefreshToken: gothUser.RefreshToken,
			ExpiresAt:    &gothUser.ExpiresAt,
		}
		if err := h.db.Create(&identity).Error; err != nil {
			return nil, err
		}
		// Update avatar if not set
		if existingUser.AvatarURL == nil && gothUser.AvatarURL != "" {
			h.db.Model(&existingUser).Update("avatar_url", gothUser.AvatarURL)
			existingUser.AvatarURL = &gothUser.AvatarURL
		}
		return &existingUser, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Create new user
	avatarURL := gothUser.AvatarURL
	newUser := user.User{
		Name:      gothUser.Name,
		Email:     gothUser.Email,
		AvatarURL: &avatarURL,
		IsAdmin:   false,
	}
	if newUser.Name == "" {
		newUser.Name = gothUser.NickName
	}
	if newUser.Name == "" {
		newUser.Name = gothUser.Email
	}
	if err := h.db.Create(&newUser).Error; err != nil {
		return nil, err
	}

	// Create OAuth identity
	identity = OAuthIdentity{
		UserID:       newUser.ID,
		Provider:     gothUser.Provider,
		ProviderID:   gothUser.UserID,
		AccessToken:  gothUser.AccessToken,
		RefreshToken: gothUser.RefreshToken,
		ExpiresAt:    &gothUser.ExpiresAt,
	}
	if err := h.db.Create(&identity).Error; err != nil {
		return nil, err
	}

	return &newUser, nil
}

func (h *Handler) updateOAuthTokens(identity *OAuthIdentity, gothUser goth.User) {
	updates := map[string]any{
		"access_token": gothUser.AccessToken,
	}
	if gothUser.RefreshToken != "" {
		updates["refresh_token"] = gothUser.RefreshToken
	}
	if !gothUser.ExpiresAt.IsZero() {
		updates["expires_at"] = gothUser.ExpiresAt
	}
	h.db.Model(identity).Updates(updates)
}
