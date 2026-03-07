package auth

import (
	"context"
	"net/http"
	"strings"

	"gorm.io/gorm"
)

// BearerTokenMiddleware returns middleware that authenticates MCP requests
// via an Authorization: Bearer <token> header.
//
// If no Authorization header is present the request passes through
// unauthenticated (MCP remains publicly accessible).
// If a header is present but invalid, a 401 response is returned.
//
// The db parameter is a function so the middleware can resolve the DB
// dynamically from the service registry (supporting reconnection).
func BearerTokenMiddleware(db func() *gorm.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				// No auth header — allow unauthenticated access.
				next.ServeHTTP(w, r)
				return
			}

			rawToken, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || rawToken == "" {
				http.Error(w, `{"error":"invalid authorization header"}`, http.StatusUnauthorized)
				return
			}

			d := db()
			if d == nil {
				http.Error(w, `{"error":"authentication unavailable"}`, http.StatusServiceUnavailable)
				return
			}

			u, err := ValidateToken(r.Context(), d, rawToken)
			if err != nil {
				http.Error(w, `{"error":"authentication error"}`, http.StatusInternalServerError)
				return
			}
			if u == nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, u)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
