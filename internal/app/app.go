package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"veloria/assets"
	"veloria/internal/auth"
	"veloria/internal/cache"
	"veloria/internal/config"
	"veloria/internal/health"
	ogimage "veloria/internal/image"
	"veloria/internal/manager"
	veloriamc "veloria/internal/mcp"
	"veloria/internal/repo"
	"veloria/internal/router"
	"veloria/internal/server"
	"veloria/internal/storage"
	"veloria/internal/tasks"
	"veloria/internal/telemetry"
	"veloria/internal/web"
)

const fmtDBString = "host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s connect_timeout=%d"

// App encapsulates the entire application lifecycle.
type App struct {
	Config       *config.Config
	Logger       *zap.Logger
	Telemetry    *telemetry.Telemetry
	DB           *gorm.DB
	SqlDB        *sql.DB
	S3           storage.ResultStorage
	Cache        cache.Cache
	Manager      *manager.Manager
	Tasks        *tasks.Tasks
	Server       *server.Server
	SessionStore *auth.SessionStore
	AuthHandler  *auth.Handler

	apiClient     *repo.APIClient
	cancelWorkers context.CancelFunc
}

// New creates and initializes a new App with all dependencies.
func New(ctx context.Context) (*App, error) {
	c, err := config.New()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if err := c.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("failed to ensure data directories: %w", err)
	}

	tel, err := telemetry.Setup(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to setup telemetry: %w", err)
	}
	l := tel.Logger

	a := &App{
		Config:    c,
		Logger:    l,
		Telemetry: tel,
	}

	// Initialize database
	if err := a.initDB(); err != nil {
		l.Error("DB initialization failed; running in no-search mode", zap.Error(err))
	}

	// Initialize S3
	if err := a.initS3(); err != nil {
		l.Error("S3 initialization failed; running in no-search mode", zap.Error(err))
	}

	// Initialize cache
	appCache, err := cache.NewRistretto()
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}
	a.Cache = appCache

	// Background workers context
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	a.cancelWorkers = cancelWorkers

	// Initialize manager and tasks if DB is available
	if a.DB != nil {
		a.Tasks = tasks.New(workerCtx)
		_ = a.Tasks.AddJob(workerCtx, "search-cleanup", tasks.CleanupStuckSearches(a.DB, l), tasks.SearchCleanupInterval)
		a.Tasks.Start()

		apiClient := repo.NewAPIClient(c.AspireCloudAPIKey, l, repo.ThrottleConfig{
			RequestsPerSecond: c.APIThrottleRPS,
			Burst:             c.APIThrottleBurst,
			MaxRetries:        c.APIThrottleMaxRetries,
			DefaultRetryDelay: c.APIThrottleRetryDelay,
		})
		a.apiClient = apiClient

		eventRecorder := repo.NewIndexEventRecorder(a.DB, l)
		_ = a.Tasks.AddJob(workerCtx, "index-event-cleanup", repo.CleanupOldEvents(a.DB, l, 30*24*time.Hour), 1*time.Hour)

		pr := repo.NewPluginStore(workerCtx, a.DB, c, l, appCache, apiClient)
		tr := repo.NewThemeStore(workerCtx, a.DB, c, l, appCache, apiClient)
		cr := repo.NewCoreStore(workerCtx, a.DB, c, l, appCache, apiClient)

		m, err := manager.NewManager(workerCtx, l, []repo.DataSource{pr, tr, cr}, c.IndexerConcurrency, eventRecorder, apiClient)
		if err != nil {
			l.Error("Failed to load repositories; running in no-search mode", zap.Error(err))
		} else {
			a.Manager = m
		}
	}

	// Setup OAuth
	auth.SetupProviders(c)

	// Initialize session store and auth handler
	if a.DB != nil {
		if c.Env != "development" && c.SessionSecret == "" {
			l.Error("SESSION_SECRET not set; continuing without auth")
		} else {
			sessionStore, err := auth.NewSessionStore(a.SqlDB, a.DB, c)
			if err != nil {
				l.Error("Failed to create session store; continuing without auth", zap.Error(err))
			} else {
				a.SessionStore = sessionStore
				a.AuthHandler = auth.NewHandler(a.DB, sessionStore, l)
			}
		}
	}

	searchEnabled := a.DB != nil && a.S3 != nil && a.Manager != nil
	searchDisabledReason := ""
	if !searchEnabled {
		switch {
		case a.DB == nil:
			searchDisabledReason = "Database connection is unavailable."
		case a.S3 == nil:
			searchDisabledReason = "Search storage is unavailable."
		default:
			searchDisabledReason = "Search index is not ready."
		}
		l.Warn("Search disabled", zap.String("reason", searchDisabledReason))
	}
	deps := web.NewDeps(a.DB, a.Manager, a.Manager, a.Manager, a.Manager, a.S3, appCache, c, searchEnabled, searchDisabledReason)

	// Initialize OG image generator
	ogGen, err := ogimage.New(assets.FS)
	if err != nil {
		l.Error("Failed to initialize OG image generator; OG images disabled", zap.Error(err))
	}

	// Build per-type stats map for the router's API list handlers.
	var statsMap map[string]manager.RepoStatsProvider
	if a.Manager != nil {
		statsMap = map[string]manager.RepoStatsProvider{
			"plugins": a.Manager.GetSource(repo.TypePlugins),
			"themes":  a.Manager.GetSource(repo.TypeThemes),
			"cores":   a.Manager.GetSource(repo.TypeCores),
		}
	}

	// Initialize MCP server
	var mcpHandler http.Handler
	if c.MCPEnabled && a.Manager != nil && a.DB != nil && a.S3 != nil {
		mcpSvc := veloriamc.NewDirectService(a.Manager, a.DB, a.S3)
		mcpServer := veloriamc.NewMCPServer(c.Name, config.Version, mcpSvc)
		mcpHandler = veloriamc.NewHTTPHandler(mcpServer)
		l.Info("MCP server enabled at /mcp")
	}

	// Health checker
	healthChecker := &health.Checker{
		DB:      a.SqlDB,
		Manager: a.Manager,
	}

	r := router.New(router.RouterDeps{
		DB:                a.DB,
		Search:            a.Manager,
		Stats:             statsMap,
		S3:                a.S3,
		WebDeps:           deps,
		Session:           a.SessionStore,
		Auth:              a.AuthHandler,
		OGGen:             ogGen,
		MCP:               mcpHandler,
		PrometheusHandler: tel.PrometheusHandler,
		HealthHandler:     health.Handler(healthChecker),
		Options: router.Options{
			HandlerTimeout:   c.HTTPHandlerTimeout,
			SearchEnabled:    searchEnabled,
			RateLimitEnabled: c.HTTPRateLimitEnabled,
			AppURL:           c.AppURL,
			RedirectDomains:  c.RedirectDomains,
		},
	})

	srv, err := server.New(r, c, l)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}
	a.Server = srv

	return a, nil
}

// Start starts the HTTP server. This blocks until the server stops.
func (a *App) Start() error {
	return a.Server.Start()
}

// Shutdown gracefully shuts down all components.
func (a *App) Shutdown(ctx context.Context) error {
	a.Logger.Info("Shutting down application")

	// Cancel background workers first
	if a.cancelWorkers != nil {
		a.cancelWorkers()
	}

	// Stop scheduled tasks
	if a.Tasks != nil {
		a.Tasks.Stop()
	}

	// Wait for the manager's updater goroutine to finish (with deadline).
	if a.Manager != nil {
		done := make(chan struct{})
		go func() {
			a.Manager.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			a.Logger.Warn("Timed out waiting for manager updater to stop")
		}
	}

	// Stop API client rate limiter
	if a.apiClient != nil {
		a.apiClient.Close()
	}

	// Shutdown HTTP servers
	if err := a.Server.Shutdown(ctx); err != nil {
		a.Logger.Error("Server shutdown failure", zap.Error(err))
	}

	// Close session store
	if a.SessionStore != nil {
		a.SessionStore.Close()
	}

	// Close cache
	if a.Cache != nil {
		a.Cache.Close()
	}

	// Close database
	if a.SqlDB != nil {
		if err := a.SqlDB.Close(); err != nil {
			a.Logger.Error("DB connection closing failure", zap.Error(err))
		}
	}

	// Shutdown telemetry (flushes traces, metrics, logs)
	if a.Telemetry != nil {
		if err := a.Telemetry.Shutdown(ctx); err != nil {
			a.Logger.Error("Telemetry shutdown failure", zap.Error(err))
		}
	}

	a.Logger.Info("Application shutdown complete")
	return nil
}

func (a *App) initDB() error {
	c := a.Config

	var logLevel gormlogger.LogLevel
	if c.AppDebug {
		logLevel = gormlogger.Info
	} else {
		logLevel = gormlogger.Error
	}

	dbLogger := gormlogger.New(log.New(os.Stderr, "\r\n", log.LstdFlags), gormlogger.Config{
		LogLevel:                  logLevel,
		IgnoreRecordNotFoundError: true,
	})

	dbString := fmt.Sprintf(fmtDBString, c.DBHost, c.DBUser, c.DBPass, c.DBName, c.DBPort, c.DBSSLMode, c.DBTimeZone, c.DBConnectTimeout)
	db, err := gorm.Open(postgres.Open(dbString), &gorm.Config{Logger: dbLogger})
	if err != nil {
		return fmt.Errorf("DB connection start failure: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get SQL DB instance: %w", err)
	}

	sqlDB.SetMaxIdleConns(c.DBMaxIdleConns)
	sqlDB.SetMaxOpenConns(c.DBMaxOpenConns)
	sqlDB.SetConnMaxIdleTime(c.DBConnMaxIdleTime)
	sqlDB.SetConnMaxLifetime(c.DBConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(context.Background(), c.DBPingTimeout)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return fmt.Errorf("DB ping failed: %w", err)
	}

	a.DB = db
	a.SqlDB = sqlDB
	return nil
}

func (a *App) initS3() error {
	c := a.Config

	s3, err := storage.NewS3Client(c, a.Logger)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}

	s3Ctx, cancel := context.WithTimeout(context.Background(), c.S3InitTimeout)
	defer cancel()

	if err := s3.EnsureBucket(s3Ctx); err != nil {
		return fmt.Errorf("failed to ensure S3 bucket exists: %w", err)
	}

	a.S3 = s3
	return nil
}
