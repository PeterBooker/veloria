package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// AccessLog returns middleware that logs every HTTP request as a structured
// Zap entry. It captures method, path, status, response size, duration,
// client IP, and the chi request ID.
func AccessLog(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			defer func() {
				status := ww.Status()
				fields := []zap.Field{
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", status),
					zap.Int("bytes", ww.BytesWritten()),
					zap.Duration("duration", time.Since(start)),
					zap.String("ip", r.RemoteAddr),
					zap.String("request_id", middleware.GetReqID(r.Context())),
				}

				msg := r.Method + " " + r.URL.Path

				switch {
				case status >= 500:
					logger.Error(msg, fields...)
				case status >= 400:
					logger.Warn(msg, fields...)
				default:
					logger.Info(msg, fields...)
				}
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
