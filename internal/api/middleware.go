package api

import (
	"net/http"
	"runtime/debug"

	"github.com/rs/zerolog/log"
)

// JSONRecoverer is a middleware that recovers from panics in API routes
// and returns a JSON error response instead of the default HTML.
func JSONRecoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				log.Error().
					Interface("panic", rvr).
					Str("stack", string(debug.Stack())).
					Msg("panic recovered in API handler")

				WriteJSON(w, ErrInternal("internal server error"))
			}
		}()

		next.ServeHTTP(w, r)
	})
}
