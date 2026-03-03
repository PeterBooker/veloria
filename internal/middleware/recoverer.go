package middleware

import (
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
)

// Recoverer returns middleware that recovers from panics and logs them as
// structured Zap errors instead of writing unstructured output to stderr
// (which is what chi's default middleware.Recoverer does).
func Recoverer(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					if rvr == http.ErrAbortHandler {
						panic(rvr) // preserve ErrAbortHandler semantics
					}

					logger.Error("panic recovered",
						zap.Any("panic", rvr),
						zap.String("method", r.Method),
						zap.String("path", r.URL.Path),
						zap.String("stack", string(debug.Stack())),
					)

					if r.Header.Get("Connection") != "Upgrade" {
						w.WriteHeader(http.StatusInternalServerError)
					}
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
