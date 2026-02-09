package router

import (
	"net/http"
	"strings"
	"time"

	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"github.com/go-playground/validator/v10"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	api "veloria/internal/api"
	"veloria/internal/auth"
	"veloria/internal/core"
	"veloria/internal/manager"
	"veloria/internal/plugin"
	"veloria/internal/search"
	"veloria/internal/storage"
	"veloria/internal/theme"
	"veloria/internal/web"
)

type Options struct {
	HandlerTimeout   time.Duration
	SearchEnabled    bool
	RateLimitEnabled bool
}

func New(l *zerolog.Logger, v *validator.Validate, db *gorm.DB, m *manager.Manager, s3 storage.ResultStorage, deps *web.Deps, sessionStore *auth.SessionStore, authHandler *auth.Handler, opts Options) *chi.Mux {
	r := chi.NewRouter()

	sentryHandler := sentryhttp.New(sentryhttp.Options{
		Repanic:         true,
		WaitForDelivery: false,
		Timeout:         2 * time.Second,
	})

	// Redirect legacy domain
	r.Use(legacyDomainRedirect)

	// Security headers
	r.Use(securityHeaders)

	// Global middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(sentryHandler.Handle)
	r.Use(middleware.RequestID)
	handlerTimeout := 5 * time.Second
	if opts.HandlerTimeout > 0 {
		handlerTimeout = opts.HandlerTimeout
	}
	r.Use(middleware.Timeout(handlerTimeout))
	r.Use(middleware.RealIP)
	r.Use(middleware.StripSlashes)

	// Auth middleware - injects user into context if logged in
	if sessionStore != nil {
		r.Use(sessionStore.AuthMiddleware)
	}

	// Health check
	r.Get("/up", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Metrics
	r.Handle("/metrics", promhttp.Handler())

	// Auth routes
	if authHandler != nil {
		r.Get("/login", web.LoginPage(deps))
		r.Get("/logout", authHandler.Logout)
		r.Route("/auth", func(r chi.Router) {
			r.Get("/{provider}", authHandler.BeginAuth)
			r.Get("/{provider}/callback", authHandler.Callback)
		})
	}

	// Web UI routes
	if deps != nil {
		r.Get("/", web.HomePage(deps))
		r.Get("/about", web.AboutPage(deps))
		r.Get("/repos", web.ReposPage(deps))
		r.Get("/repos/{type}", web.RepoPage(deps))
		r.Get("/repos/plugins/{slug}", plugin.ViewPage(deps))
		r.Get("/repos/themes/{slug}", theme.ViewPage(deps))
		r.Get("/repos/cores/{version}", core.ViewPage(deps))
		r.Get("/searches", search.ListPage(deps))
		r.Get("/search/{uuid}", search.ViewPage(deps))
		r.Get("/search/{uuid}/context", search.ContextPage(deps))
		r.Get("/search/{uuid}/extension/{slug}", search.ExtensionResultsPage(deps))
		r.Post("/search", search.SubmitSearch(deps))
		r.Get("/my-searches", search.MyListPage(deps))
	}

	// JSON API routes
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"))
		r.Use(api.JSONRecoverer)
		if opts.RateLimitEnabled {
			r.Use(httprate.LimitByIP(100, time.Minute))
		}

		r.Route("/v1", func(r chi.Router) {
			r.Route("/plugin", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", plugin.ViewPluginV1(db))
			})

			r.Route("/plugins", func(r chi.Router) {
				if m != nil {
					r.Method(http.MethodGet, "/", plugin.ListPluginsV1(db, m.GetPluginRepo()))
				} else {
					r.Method(http.MethodGet, "/", plugin.ListPluginsV1(db, nil))
				}
			})

			r.Route("/theme", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", theme.ViewThemeV1(db))
			})

			r.Route("/themes", func(r chi.Router) {
				if m != nil {
					r.Method(http.MethodGet, "/", theme.ListThemesV1(db, m.GetThemeRepo()))
				} else {
					r.Method(http.MethodGet, "/", theme.ListThemesV1(db, nil))
				}
			})

			r.Route("/core", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", core.ViewCoreV1(db))
			})

			r.Route("/cores", func(r chi.Router) {
				if m != nil {
					r.Method(http.MethodGet, "/", core.ListCoresV1(db, m.GetCoreRepo()))
				} else {
					r.Method(http.MethodGet, "/", core.ListCoresV1(db, nil))
				}
			})

			r.Route("/search", func(r chi.Router) {
				if opts.RateLimitEnabled {
					r.Use(httprate.LimitByIP(10, time.Minute))
				}
				r.Method(http.MethodPost, "/", search.CreateSearchV1(db, m, s3))
				r.Method(http.MethodGet, "/{id}", search.ViewSearchV1(db, s3))
			})

			r.Route("/searches", func(r chi.Router) {
				r.Method(http.MethodGet, "/", search.ListSearchesV1(db))
			})
		})
	})

	return r
}

// securityHeaders sets common security headers on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// legacyDomainRedirect redirects requests from wpdirectory.net to veloria.dev.
// Search URLs are preserved; all other paths redirect to root.
func legacyDomainRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "wpdirectory.net" || r.Host == "www.wpdirectory.net" {
			target := "https://veloria.dev"
			if strings.HasPrefix(r.URL.Path, "/search/") {
				target += r.URL.Path
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}
