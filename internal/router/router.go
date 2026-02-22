package router

import (
	"net/http"
	"strings"
	"time"

	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"veloria/internal/admin"
	api "veloria/internal/api"
	"veloria/internal/auth"
	"veloria/internal/core"
	"veloria/internal/logger"
	"veloria/internal/manager"
	"veloria/internal/plugin"
	"veloria/internal/report"
	"veloria/internal/search"
	"veloria/internal/storage"
	"veloria/internal/theme"
	"veloria/internal/web"
)

// Options holds runtime configuration for the router.
type Options struct {
	HandlerTimeout   time.Duration
	SearchEnabled    bool
	RateLimitEnabled bool
	LoggingEnabled   bool
	AppURL           string   // Target URL for legacy domain redirects (e.g., "https://veloria.dev")
	RedirectDomains  []string // Legacy domains to redirect (e.g., ["wpdirectory.net", "www.wpdirectory.net"])
}

// RouterDeps holds all dependencies needed to construct the router.
// Optional fields may be nil when the corresponding subsystem is unavailable.
type RouterDeps struct {
	Logger  *zerolog.Logger
	DB      *gorm.DB
	Search  manager.Searcher                       // for API search endpoint; or nil
	Stats   map[string]manager.RepoStatsProvider   // per-type stats for API list endpoints; or nil
	S3      storage.ResultStorage
	WebDeps *web.Deps
	Session *auth.SessionStore
	Auth    *auth.Handler
	Options Options
}

func New(deps RouterDeps) *chi.Mux {
	r := chi.NewRouter()

	sentryHandler := sentryhttp.New(sentryhttp.Options{
		Repanic:         true,
		WaitForDelivery: false,
		Timeout:         2 * time.Second,
	})

	opts := deps.Options

	// Redirect legacy domains
	if opts.AppURL != "" && len(opts.RedirectDomains) > 0 {
		r.Use(legacyDomainRedirect(opts.AppURL, opts.RedirectDomains))
	}

	// Security headers
	r.Use(securityHeaders)

	// Access logging (stdout)
	if opts.LoggingEnabled {
		r.Use(logger.AccessLogger)
	}
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
	if deps.Session != nil {
		r.Use(deps.Session.AuthMiddleware)
	}

	// Health check
	r.Get("/up", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Metrics
	r.Handle("/metrics", promhttp.Handler())

	// Auth routes
	if deps.Auth != nil {
		r.Get("/login", web.LoginPage(deps.WebDeps))
		r.Get("/logout", deps.Auth.Logout)
		r.Route("/auth", func(r chi.Router) {
			r.Get("/{provider}", deps.Auth.BeginAuth)
			r.Get("/{provider}/callback", deps.Auth.Callback)
		})
	}

	// Web UI routes
	if deps.WebDeps != nil {
		r.Get("/", web.HomePage(deps.WebDeps))
		r.Get("/about", web.AboutPage(deps.WebDeps))
		r.Get("/repos", web.ReposPage(deps.WebDeps))
		r.Get("/repos/{type}", web.RepoPage(deps.WebDeps))
		r.Get("/repos/plugins/items", web.RepoItemsPartial(deps.WebDeps, "plugins"))
		r.Get("/repos/themes/items", web.RepoItemsPartial(deps.WebDeps, "themes"))
		r.Get("/repos/cores/items", web.RepoItemsPartial(deps.WebDeps, "cores"))
		r.Get("/repos/plugins/{slug}", plugin.ViewPage(deps.WebDeps))
		r.Get("/repos/themes/{slug}", theme.ViewPage(deps.WebDeps))
		r.Get("/repos/cores/{version}", core.ViewPage(deps.WebDeps))
		r.Get("/searches", search.ListPage(deps.WebDeps))
		r.Get("/search/{uuid}", search.ViewPage(deps.WebDeps))
		r.Get("/search/{uuid}/context", search.ContextPage(deps.WebDeps))
		r.Get("/search/{uuid}/extensions", search.SearchExtensionsPartial(deps.WebDeps))
		r.Get("/search/{uuid}/extension/{slug}", search.ExtensionResultsPage(deps.WebDeps))
		r.Get("/search/{uuid}/export", search.ExportCSV(deps.WebDeps))
		r.Post("/search", search.SubmitSearch(deps.WebDeps))
		r.Get("/my-searches", search.MyListPage(deps.WebDeps))

		// Report a search (requires login)
		if deps.Session != nil {
			r.With(deps.Session.RequireAuth).Post("/search/{uuid}/report", report.SubmitReport(deps.WebDeps))
		}

		// Admin routes
		if deps.Session != nil {
			r.Route("/admin", func(r chi.Router) {
				r.Use(deps.Session.RequireAuth)
				r.Use(deps.Session.RequireAdmin)

				r.Get("/reports", report.ReportsPage(deps.WebDeps))
				r.Post("/reports/{id}/resolve", report.ResolveReport(deps.WebDeps))
				r.Post("/search/{uuid}/visibility", search.ToggleVisibility(deps.WebDeps))
				r.Post("/reindex", admin.ReindexExtension(deps.WebDeps))
			})
		}
	}

	// Helper to get per-type stats provider (nil-safe).
	statsFor := func(repoType string) manager.RepoStatsProvider {
		if deps.Stats != nil {
			return deps.Stats[repoType]
		}
		return nil
	}

	db := deps.DB

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
				r.Method(http.MethodGet, "/", plugin.ListPluginsV1(db, statsFor("plugins")))
			})

			r.Route("/theme", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", theme.ViewThemeV1(db))
			})

			r.Route("/themes", func(r chi.Router) {
				r.Method(http.MethodGet, "/", theme.ListThemesV1(db, statsFor("themes")))
			})

			r.Route("/core", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", core.ViewCoreV1(db))
			})

			r.Route("/cores", func(r chi.Router) {
				r.Method(http.MethodGet, "/", core.ListCoresV1(db, statsFor("cores")))
			})

			r.Route("/search", func(r chi.Router) {
				if opts.RateLimitEnabled {
					r.Use(httprate.LimitByIP(10, time.Minute))
				}
				r.Method(http.MethodPost, "/", search.CreateSearchV1(db, deps.Search, deps.S3))
				r.Method(http.MethodGet, "/{id}", search.ViewSearchV1(db, deps.S3))
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

// legacyDomainRedirect returns middleware that redirects requests from legacy
// domains to the primary app URL. Search URLs are preserved; all other paths
// redirect to root.
func legacyDomainRedirect(appURL string, domains []string) func(http.Handler) http.Handler {
	domainSet := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		domainSet[d] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := domainSet[r.Host]; ok {
				target := appURL
				if strings.HasPrefix(r.URL.Path, "/search/") {
					target += r.URL.Path
				}
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
