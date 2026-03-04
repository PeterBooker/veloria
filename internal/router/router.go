package router

import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"go.uber.org/zap"

	"veloria/assets"
	"veloria/internal/admin"
	api "veloria/internal/api"
	"veloria/internal/core"
	ogimage "veloria/internal/image"
	veloriamd "veloria/internal/middleware"
	"veloria/internal/plugin"
	"veloria/internal/report"
	"veloria/internal/search"
	"veloria/internal/service"
	"veloria/internal/theme"
	"veloria/internal/web"
)

// Options holds runtime configuration for the router.
type Options struct {
	HandlerTimeout   time.Duration
	RateLimitEnabled bool
	AppURL           string   // Target URL for legacy domain redirects (e.g., "https://veloria.dev")
	RedirectDomains  []string // Legacy domains to redirect (e.g., ["wpdirectory.net", "www.wpdirectory.net"])
	MCPEnabled       bool
}

// RouterDeps holds all dependencies needed to construct the router.
// Optional fields may be nil when the corresponding subsystem is unavailable.
type RouterDeps struct {
	Logger            *zap.Logger
	Registry          *service.Registry
	WebDeps           *web.Deps
	OGGen             *ogimage.Generator // OG image generator; or nil
	PrometheusHandler http.Handler       // Prometheus metrics handler; or nil
	HealthHandler     http.HandlerFunc   // Health readiness endpoint; or nil
	Options           Options
}

func New(deps RouterDeps) *chi.Mux {
	r := chi.NewRouter()

	opts := deps.Options
	reg := deps.Registry

	// Maintenance middleware — must be first so it intercepts all requests.
	r.Use(maintenanceMiddleware(reg))

	// Redirect legacy domains
	if opts.AppURL != "" && len(opts.RedirectDomains) > 0 {
		r.Use(legacyDomainRedirect(opts.AppURL, opts.RedirectDomains))
	}

	// Security headers
	r.Use(securityHeaders)

	r.Use(veloriamd.Recoverer(deps.Logger))
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(veloriamd.AccessLog(deps.Logger))
	handlerTimeout := 5 * time.Second
	if opts.HandlerTimeout > 0 {
		handlerTimeout = opts.HandlerTimeout
	}
	r.Use(handlerTimeoutSkipMCP(handlerTimeout))
	r.Use(middleware.StripSlashes)

	// Auth middleware - dynamically resolved from registry
	r.Use(dynamicAuthMiddleware(reg))

	// Liveness check
	r.Get("/up", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Readiness / health check
	if deps.HealthHandler != nil {
		r.Get("/health", deps.HealthHandler)
	}

	// Metrics
	if deps.PrometheusHandler != nil {
		r.Handle("/metrics", deps.PrometheusHandler)
	}

	// Auth routes - dynamically resolved
	r.Get("/login", dynamicLoginHandler(reg, deps.WebDeps))
	r.Get("/logout", dynamicLogoutHandler(reg))
	r.Route("/auth", func(r chi.Router) {
		r.Get("/{provider}", dynamicBeginAuth(reg))
		r.Get("/{provider}/callback", dynamicAuthCallback(reg))
	})

	// Static assets (CSS, JS, fonts)
	staticFS, _ := fs.Sub(assets.FS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", cacheStatic(http.FileServerFS(staticFS))))

	// Static OG default image
	r.Get("/og-default.png", web.StaticImage(assets.FS, "og-default.png"))

	// Favicons (served at root to satisfy automatic browser requests)
	r.Get("/favicon.ico", web.StaticImage(assets.FS, "static/favicon.ico"))
	r.Get("/favicon.svg", web.StaticImage(assets.FS, "static/favicon.svg"))

	// Web UI routes
	if deps.WebDeps != nil {
		r.Get("/", web.HomePage(deps.WebDeps))
		r.Get("/about", web.AboutPage(deps.WebDeps))
		r.Get("/privacy", web.PrivacyPage(deps.WebDeps))
		r.Get("/terms", web.TermsPage(deps.WebDeps))
		r.Get("/docs", web.DocsPage(deps.WebDeps))
		r.Get("/data-sources", web.DataSourcesPage(deps.WebDeps))
		r.Get("/data-sources/{type}", web.DataSourcePage(deps.WebDeps))
		r.Get("/data-sources/plugins/items", web.DataSourceItemsPartial(deps.WebDeps, "plugins"))
		r.Get("/data-sources/themes/items", web.DataSourceItemsPartial(deps.WebDeps, "themes"))
		r.Get("/data-sources/cores/items", web.DataSourceItemsPartial(deps.WebDeps, "cores"))
		r.Get("/data-sources/plugins/failed-indexing", web.FailedIndexPartial(deps.WebDeps, "plugins"))
		r.Get("/data-sources/themes/failed-indexing", web.FailedIndexPartial(deps.WebDeps, "themes"))
		r.Get("/data-sources/cores/failed-indexing", web.FailedIndexPartial(deps.WebDeps, "cores"))
		r.Get("/data-sources/plugins/{slug}", plugin.ViewPage(deps.WebDeps))
		r.Get("/data-sources/themes/{slug}", theme.ViewPage(deps.WebDeps))
		r.Get("/data-sources/cores/{version}", core.ViewPage(deps.WebDeps))
		r.Get("/searches", search.ListPage(deps.WebDeps))
		r.Get("/search/{uuid}", search.ViewPage(deps.WebDeps))
		r.Get("/search/{uuid}/context", search.ContextPage(deps.WebDeps))
		r.Get("/search/{uuid}/extensions", search.SearchExtensionsPartial(deps.WebDeps))
		r.Get("/search/{uuid}/extension/{slug}", search.ExtensionResultsPage(deps.WebDeps))
		r.Get("/search/{uuid}/export", search.ExportCSV(deps.WebDeps))
		if deps.OGGen != nil {
			ogHandler := search.OGImage(deps.WebDeps, deps.OGGen)
			if opts.RateLimitEnabled {
				r.With(httprate.LimitByIP(60, time.Minute)).Get("/search/{uuid}/og.png", ogHandler)
			} else {
				r.Get("/search/{uuid}/og.png", ogHandler)
			}
		}
		r.Post("/search", search.SubmitSearch(deps.WebDeps))
		r.Get("/searches/own", search.ListOwnPage(deps.WebDeps))
		r.Get("/my-searches", search.MyListRedirect())

		// Report a search (requires login - dynamically checked)
		r.With(dynamicRequireAuth(reg)).Post("/search/{uuid}/report", report.SubmitReport(deps.WebDeps))

		// Admin routes
		r.Route("/admin", func(r chi.Router) {
			r.Use(dynamicRequireAuth(reg))
			r.Use(dynamicRequireAdmin(reg))

			r.Get("/reports", report.ReportsPage(deps.WebDeps))
			r.Post("/reports/{id}/resolve", report.ResolveReport(deps.WebDeps))
			r.Post("/search/{uuid}/visibility", search.ToggleVisibility(deps.WebDeps))
			r.Post("/reindex", admin.ReindexExtension(deps.WebDeps))
			r.Post("/maintenance", admin.ToggleMaintenance(reg))
		})
	}

	// MCP (Model Context Protocol) endpoint - dynamically resolved.
	// Exempt from handler timeout (streaming transport) with extended write deadline.
	if opts.MCPEnabled {
		mcpHandler := dynamicMCPHandler(reg)
		if opts.RateLimitEnabled {
			r.With(mcpWriteDeadline, httprate.LimitByIP(100, time.Minute)).Mount("/mcp", mcpHandler)
		} else {
			r.With(mcpWriteDeadline).Mount("/mcp", mcpHandler)
		}
	}

	// JSON API routes - use registry for dynamic resolution
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"))
		r.Use(api.JSONRecoverer)
		if opts.RateLimitEnabled {
			r.Use(httprate.LimitByIP(100, time.Minute))
		}

		r.Route("/v1", func(r chi.Router) {
			r.Route("/plugin", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", plugin.ViewPluginV1(reg))
			})

			r.Route("/plugins", func(r chi.Router) {
				r.Method(http.MethodGet, "/", plugin.ListPluginsV1(reg))
			})

			r.Route("/theme", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", theme.ViewThemeV1(reg))
			})

			r.Route("/themes", func(r chi.Router) {
				r.Method(http.MethodGet, "/", theme.ListThemesV1(reg))
			})

			r.Route("/core", func(r chi.Router) {
				r.Method(http.MethodGet, "/{id}", core.ViewCoreV1(reg))
			})

			r.Route("/cores", func(r chi.Router) {
				r.Method(http.MethodGet, "/", core.ListCoresV1(reg))
			})

			r.Route("/search", func(r chi.Router) {
				if opts.RateLimitEnabled {
					r.Use(httprate.LimitByIP(10, time.Minute))
				}
				r.Method(http.MethodPost, "/", search.CreateSearchV1(reg))
				r.Method(http.MethodGet, "/{id}", search.ViewSearchV1(reg))
			})

			r.Route("/searches", func(r chi.Router) {
				r.Method(http.MethodGet, "/", search.ListSearchesV1(reg))
			})
		})
	})

	return r
}

// maintenanceMiddleware returns 503 for all requests (except health/liveness)
// when maintenance mode is active.
func maintenanceMiddleware(reg *service.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !reg.InMaintenance() {
				next.ServeHTTP(w, r)
				return
			}
			// Allow health/liveness/admin endpoints through
			switch {
			case r.URL.Path == "/up", r.URL.Path == "/health", r.URL.Path == "/metrics":
				next.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/admin/"):
				next.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/static/"),
				r.URL.Path == "/favicon.ico",
				r.URL.Path == "/favicon.svg":
				next.ServeHTTP(w, r)
			case strings.HasPrefix(r.URL.Path, "/api/"), strings.HasPrefix(r.URL.Path, "/mcp"):
				api.WriteJSON(w, api.ErrUnavailable("Service is undergoing maintenance"))
			default:
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Retry-After", "300")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write(maintenanceHTML)
			}
		})
	}
}

// dynamicAuthMiddleware injects user into context if session store is available.
func dynamicAuthMiddleware(reg *service.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s := reg.Session(); s != nil {
				s.AuthMiddleware(next).ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// dynamicRequireAuth returns middleware that requires authentication,
// resolving the session store from the registry at request time.
func dynamicRequireAuth(reg *service.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s := reg.Session(); s != nil {
				s.RequireAuth(next).ServeHTTP(w, r)
				return
			}
			http.Error(w, "Authentication unavailable", http.StatusServiceUnavailable)
		})
	}
}

// dynamicRequireAdmin returns middleware that requires admin role,
// resolving the session store from the registry at request time.
func dynamicRequireAdmin(reg *service.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s := reg.Session(); s != nil {
				s.RequireAdmin(next).ServeHTTP(w, r)
				return
			}
			http.Error(w, "Authentication unavailable", http.StatusServiceUnavailable)
		})
	}
}

func dynamicLoginHandler(reg *service.Registry, d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg.Auth() == nil {
			http.Error(w, "Authentication unavailable", http.StatusServiceUnavailable)
			return
		}
		web.LoginPage(d)(w, r)
	}
}

func dynamicLogoutHandler(reg *service.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h := reg.Auth(); h != nil {
			h.Logout(w, r)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func dynamicBeginAuth(reg *service.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h := reg.Auth(); h != nil {
			h.BeginAuth(w, r)
			return
		}
		http.Error(w, "Authentication unavailable", http.StatusServiceUnavailable)
	}
}

func dynamicAuthCallback(reg *service.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h := reg.Auth(); h != nil {
			h.Callback(w, r)
			return
		}
		http.Error(w, "Authentication unavailable", http.StatusServiceUnavailable)
	}
}

func dynamicMCPHandler(reg *service.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := reg.MCPHandler(); h != nil {
			h.ServeHTTP(w, r)
			return
		}
		api.WriteJSON(w, api.ErrUnavailable("MCP unavailable"))
	})
}

// maintenanceHTML is the static HTML served during maintenance mode.
var maintenanceHTML = []byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Maintenance - Veloria</title>
<style>
body{font-family:system-ui,-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f8fafc;color:#334155}
.box{text-align:center;max-width:480px;padding:2rem}
h1{font-size:1.5rem;margin-bottom:.5rem}
p{color:#64748b;line-height:1.6}
</style>
</head>
<body>
<div class="box">
<h1>We'll be right back</h1>
<p>Veloria is undergoing scheduled maintenance. Please check back shortly.</p>
</div>
</body>
</html>`)

// cacheStatic wraps a handler with long-lived cache headers for static assets.
func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		h.ServeHTTP(w, r)
	})
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

// handlerTimeoutSkipMCP applies chi's Timeout middleware to all routes except
// /mcp, which uses a streaming transport that requires long-lived connections.
func handlerTimeoutSkipMCP(timeout time.Duration) func(http.Handler) http.Handler {
	tm := middleware.Timeout(timeout)
	return func(next http.Handler) http.Handler {
		withTimeout := tm(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/mcp") {
				next.ServeHTTP(w, r)
				return
			}
			withTimeout.ServeHTTP(w, r)
		})
	}
}

// mcpWriteDeadline extends the server's WriteTimeout for MCP requests.
// The default WriteTimeout is too short for streaming MCP tool calls.
func mcpWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))
		next.ServeHTTP(w, r)
	})
}
