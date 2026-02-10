package logger

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

var accessLog = log.New(os.Stdout, "", log.LstdFlags)

// AccessLogger is an HTTP middleware that writes access logs to stdout in the format:
//
//	2026/02/10 18:01:02 "GET https://veloria.dev/path HTTP/2.0" from 1.2.3.4:5678 - 200 1234B in 1.234ms
func AccessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		}

		accessLog.Printf("\"%s %s://%s%s %s\" from %s - %d %dB in %s",
			r.Method,
			scheme,
			r.Host,
			r.RequestURI,
			r.Proto,
			r.RemoteAddr,
			ww.Status(),
			ww.BytesWritten(),
			time.Since(start),
		)
	})
}
