package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"veloria/internal/config"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
	"github.com/rs/zerolog"
)

// Server manages the HTTP server lifecycle including optional TLS via certmagic.
type Server struct {
	main     *http.Server
	redirect *http.Server
	tlsLn    net.Listener
	logger   *zerolog.Logger
}

// New creates a server configured based on the environment.
// In production with APP_URL set: HTTPS on :443 with certmagic + HTTP redirect on :80.
// Otherwise: plain HTTP on the configured port.
func New(handler http.Handler, c *config.Config, l *zerolog.Logger) (*Server, error) {
	s := &Server{
		logger: l,
		main: &http.Server{
			Handler:           handler,
			ReadTimeout:       c.HTTPReadTimeout,
			ReadHeaderTimeout: c.HTTPReadHeaderTimeout,
			WriteTimeout:      c.HTTPWriteTimeout,
			IdleTimeout:       c.HTTPIdleTimeout,
		},
	}

	if c.Env != "development" && c.AppURL != "" {
		u, err := url.Parse(c.AppURL)
		if err != nil || u.Hostname() == "" {
			return nil, fmt.Errorf("invalid APP_URL %q; must be a full URL (e.g., https://example.com)", c.AppURL)
		}
		domain := u.Hostname()

		certmagic.Default.Storage = &certmagic.FileStorage{Path: filepath.Join(c.DataDir, "certs")}
		certmagic.DefaultACME.Agreed = true
		certmagic.DefaultACME.DisableHTTPChallenge = true
		certmagic.DefaultACME.DisableTLSALPNChallenge = true
		certmagic.DefaultACME.DNS01Solver = &certmagic.DNS01Solver{
			DNSManager: certmagic.DNSManager{
				DNSProvider: &cloudflare.Provider{
					APIToken: c.CloudflareAPIToken,
				},
			},
		}
		if c.ACMEEmail != "" {
			certmagic.DefaultACME.Email = c.ACMEEmail
		}

		cfg := certmagic.NewDefault()
		if err := cfg.ManageSync(context.Background(), []string{domain}); err != nil {
			return nil, fmt.Errorf("failed to manage TLS certificate for %s: %w", domain, err)
		}

		tlsCfg := cfg.TLSConfig()
		tlsCfg.NextProtos = append([]string{"h2", "http/1.1"}, tlsCfg.NextProtos...)
		s.main.TLSConfig = tlsCfg
		s.main.Handler = hstsHandler(handler)

		s.tlsLn, err = tls.Listen("tcp", ":443", tlsCfg) // #nosec G102 -- intentional bind to all interfaces for public server
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS listener: %w", err)
		}

		s.redirect = &http.Server{
			Addr: ":80",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				target := "https://" + req.Host + req.URL.RequestURI()
				http.Redirect(w, req, target, http.StatusMovedPermanently)
			}),
			ReadTimeout:       5 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      5 * time.Second,
			IdleTimeout:       5 * time.Second,
		}

		l.Info().Msgf("Managing TLS certificate for %s", domain)
	} else {
		if c.Env != "development" && c.AppURL == "" {
			l.Warn().Msg("APP_URL not set; running without TLS")
		}
		s.main.Addr = fmt.Sprintf(":%d", c.Port)
	}

	return s, nil
}

// Start begins serving. It starts the HTTP redirect server in a goroutine
// (if configured) and blocks on the main server.
func (s *Server) Start() error {
	if s.redirect != nil {
		go func() {
			s.logger.Info().Msg("Starting HTTP redirect server on :80")
			if err := s.redirect.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.logger.Error().Err(err).Msg("HTTP redirect server failure")
			}
		}()
	}

	if s.tlsLn != nil {
		s.logger.Info().Msgf("Starting HTTPS server on :443")
		if err := s.main.Serve(s.tlsLn); err != nil && err != http.ErrServerClosed {
			return err
		}
	} else {
		s.logger.Info().Msgf("Starting server %v", s.main.Addr)
		if err := s.main.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
	}

	return nil
}

// hstsHandler wraps a handler to set the Strict-Transport-Security header.
func hstsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// Shutdown gracefully stops all servers.
func (s *Server) Shutdown(ctx context.Context) error {
	var firstErr error
	if s.redirect != nil {
		if err := s.redirect.Shutdown(ctx); err != nil {
			s.logger.Error().Err(err).Msg("HTTP redirect server shutdown failure")
			firstErr = err
		}
	}
	if err := s.main.Shutdown(ctx); err != nil {
		s.logger.Error().Err(err).Msg("Server shutdown failure")
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
