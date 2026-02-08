package auth

import (
	"context"
	"net/http"
)

// AuthMiddleware injects user into context if logged in (does not require auth).
func (s *SessionStore) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := s.GetUser(r)
		if user != nil {
			ctx := context.WithValue(r.Context(), UserContextKey, user)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth middleware requires user to be logged in.
func (s *SessionStore) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			// For HTMX requests, return 401
			if r.Header.Get("HX-Request") == "true" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			// For regular requests, redirect to login
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin middleware requires user to be admin.
func (s *SessionStore) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
