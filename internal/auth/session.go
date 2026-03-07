package auth

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/antonlindstrom/pgstore"
	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/markbates/goth/gothic"
	"gorm.io/gorm"

	"veloria/internal/config"
	"veloria/internal/user"
)

type contextKey string

// UserContextKey is the context key for storing the current user.
const UserContextKey contextKey = "user"

const sessionName = "veloria-session"

// SessionStore manages user sessions.
type SessionStore struct {
	store *pgstore.PGStore
	db    *gorm.DB
}

// NewSessionStore creates a new session store using PostgreSQL.
func NewSessionStore(sqlDB *sql.DB, db *gorm.DB, cfg *config.Config) (*SessionStore, error) {
	store, err := pgstore.NewPGStoreFromPool(sqlDB, []byte(cfg.SessionSecret))
	if err != nil {
		return nil, err
	}

	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   cfg.Env != "development",
		SameSite: http.SameSiteLaxMode,
	}

	// Configure gothic to use our session store
	gothic.Store = store

	return &SessionStore{store: store, db: db}, nil
}

// Close cleans up the session store.
func (s *SessionStore) Close() {
	if s.store != nil {
		s.store.Close()
	}
}

// SetUser stores the user ID in the session.
func (s *SessionStore) SetUser(w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	session, _ := s.store.Get(r, sessionName)
	session.Values["user_id"] = userID.String()
	return session.Save(r, w)
}

// GetUser retrieves the current user from the session.
func (s *SessionStore) GetUser(r *http.Request) (*user.User, error) {
	session, err := s.store.Get(r, sessionName)
	if err != nil {
		return nil, nil
	}

	userIDStr, ok := session.Values["user_id"].(string)
	if !ok || userIDStr == "" {
		return nil, nil
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, nil
	}

	var u user.User
	if err := s.db.First(&u, "id = ? AND deleted_at IS NULL", userID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &u, nil
}

// ClearSession removes the user from the session.
func (s *SessionStore) ClearSession(w http.ResponseWriter, r *http.Request) error {
	session, _ := s.store.Get(r, sessionName)
	session.Values["user_id"] = ""
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

// UserFromContext retrieves the user from request context.
func UserFromContext(ctx context.Context) *user.User {
	u, _ := ctx.Value(UserContextKey).(*user.User)
	return u
}
