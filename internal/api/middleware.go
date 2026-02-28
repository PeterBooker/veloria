package api

import (
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
)

// JSONRecoverer is a middleware that recovers from panics in API routes
// and returns a JSON error response instead of the default HTML.
func JSONRecoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				zap.L().Error("panic recovered in API handler",
					zap.Any("panic", rvr),
					zap.String("stack", string(debug.Stack())),
				)

				WriteJSON(w, ErrInternal("internal server error"))
			}
		}()

		next.ServeHTTP(w, r)
	})
}
