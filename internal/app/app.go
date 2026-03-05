package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
	"veloria/internal/service"
	"veloria/internal/storage"
	"veloria/internal/tasks"
	"veloria/internal/telemetry"
	"veloria/internal/web"
)

const fmtDBString = "host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s connect_timeout=%d"

// App encapsulates the entire application lifecycle.
type App struct {
	Config    *config.Config
	Logger    *zap.Logger
	Telemetry *telemetry.Telemetry
	Cache     cache.Cache
	Server    *server.Server
	Registry  *service.Registry

	cancelWorkers context.CancelFunc
	workerCtx     context.Context
	ctlListener   net.Listener // Unix socket for maintenance CLI
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

	reg := &service.Registry{}

	a := &App{
		Config:    c,
		Logger:    l,
		Telemetry: tel,
		Registry:  reg,
	}

	// Initialize database
	if err := a.initDB(); err != nil {
		l.Error("DB initialization failed; running in degraded mode", zap.Error(err))
	}

	// Initialize S3
	if err := a.initS3(); err != nil {
		l.Error("S3 initialization failed; running in degraded mode", zap.Error(err))
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
	a.workerCtx = workerCtx

	// Initialize DB-dependent services (manager, tasks, session, auth, MCP)
	if reg.DB() != nil {
		a.initDBDependents(workerCtx, appCache)
	}

	// Setup OAuth providers (does not require DB)
	auth.SetupProviders(c)

	if !reg.SearchEnabled() {
		l.Warn("Search disabled", zap.String("reason", reg.SearchDisabledReason()))
	}

	deps := web.NewDeps(reg, appCache, c)

	// Initialize OG image generator
	ogGen, err := ogimage.New(assets.FS)
	if err != nil {
		l.Error("Failed to initialize OG image generator; OG images disabled", zap.Error(err))
	}

	// Health checker
	healthChecker := &health.Checker{
		Registry: reg,
	}

	r := router.New(router.RouterDeps{
		Logger:            l,
		Registry:          reg,
		WebDeps:           deps,
		OGGen:             ogGen,
		PrometheusHandler: tel.PrometheusHandler,
		HealthHandler:     health.Handler(healthChecker),
		Options: router.Options{
			HandlerTimeout:   c.HTTPHandlerTimeout,
			RateLimitEnabled: c.HTTPRateLimitEnabled,
			AppURL:           c.AppURL,
			RedirectDomains:  c.RedirectDomains,
			MCPEnabled:       c.MCPEnabled,
		},
	})

	srv, err := server.New(r, c, l)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}
	a.Server = srv

	// Start background reconnection loop if any service failed to connect
	if reg.DB() == nil || reg.S3() == nil {
		go a.reconnectLoop(workerCtx)
	}

	// Start Unix control socket for maintenance CLI
	if err := a.startControlSocket(); err != nil {
		l.Error("Failed to start control socket; CLI maintenance commands unavailable", zap.Error(err))
	}

	return a, nil
}

// initDBDependents initializes all services that depend on the database:
// tasks, API client, data stores, manager, session store, auth handler, and MCP.
func (a *App) initDBDependents(ctx context.Context, appCache cache.Cache) {
	c := a.Config
	l := a.Logger
	reg := a.Registry
	db := reg.DB()

	t := tasks.New(ctx)
	t.AddJob(tasks.CleanupStuckSearches(db, l), tasks.SearchCleanupInterval)
	t.Start()
	reg.SetTasks(t)

	apiClient := repo.NewAPIClient(c.AspireCloudAPIKey, l, repo.ThrottleConfig{
		RequestsPerSecond: c.APIThrottleRPS,
		Burst:             c.APIThrottleBurst,
		MaxRetries:        c.APIThrottleMaxRetries,
		DefaultRetryDelay: c.APIThrottleRetryDelay,
	})
	reg.SetAPIClient(apiClient)

	eventRecorder := repo.NewIndexEventRecorder(db, l)
	t.AddJob(repo.CleanupOldEvents(db, l, 30*24*time.Hour), 1*time.Hour)

	pr := repo.NewPluginStore(ctx, db, c, l, appCache, apiClient)
	tr := repo.NewThemeStore(ctx, db, c, l, appCache, apiClient)
	cr := repo.NewCoreStore(ctx, db, c, l, appCache, apiClient)

	m, err := manager.NewManager(ctx, l, []repo.DataSource{pr, tr, cr}, c.IndexerConcurrency, eventRecorder, apiClient, appCache)
	if err != nil {
		l.Error("Failed to load repositories; running in degraded mode", zap.Error(err))
	} else {
		reg.SetManager(m)
	}

	// Session store and auth handler
	if c.Env != "development" && c.SessionSecret == "" {
		l.Error("SESSION_SECRET not set; continuing without auth")
	} else {
		sessionStore, err := auth.NewSessionStore(reg.SqlDB(), db, c)
		if err != nil {
			l.Error("Failed to create session store; continuing without auth", zap.Error(err))
		} else {
			reg.SetSession(sessionStore)
			reg.SetAuth(auth.NewHandler(db, sessionStore, l))
		}
	}

	// MCP server
	if c.MCPEnabled && reg.Manager() != nil && reg.S3() != nil {
		mcpSvc := veloriamc.NewDirectService(reg.Manager(), db, reg.S3())
		mcpServer := veloriamc.NewMCPServer(c.Name, config.Version, mcpSvc)
		reg.SetMCPHandler(veloriamc.NewHTTPHandler(mcpServer))
		l.Info("MCP server enabled at /mcp")
	}
}

// Start starts the HTTP server. This blocks until the server stops.
func (a *App) Start() error {
	return a.Server.Start()
}

// Shutdown gracefully shuts down all components.
func (a *App) Shutdown(ctx context.Context) error {
	a.Logger.Info("Shutting down application")

	// Close control socket
	if a.ctlListener != nil {
		_ = a.ctlListener.Close()
	}

	// Cancel background workers first
	if a.cancelWorkers != nil {
		a.cancelWorkers()
	}

	// Stop scheduled tasks
	if t := a.Registry.Tasks(); t != nil {
		t.Stop()
	}

	// Wait for the manager's updater goroutine to finish (with deadline).
	if m := a.Registry.Manager(); m != nil {
		done := make(chan struct{})
		go func() {
			m.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			a.Logger.Warn("Timed out waiting for manager updater to stop")
		}
	}

	// Stop API client rate limiter
	if c := a.Registry.APIClient(); c != nil {
		c.Close()
	}

	// Shutdown HTTP servers
	if err := a.Server.Shutdown(ctx); err != nil {
		a.Logger.Error("Server shutdown failure", zap.Error(err))
	}

	// Close session store
	if s := a.Registry.Session(); s != nil {
		s.Close()
	}

	// Close cache
	if a.Cache != nil {
		a.Cache.Close()
	}

	// Close database
	if sqlDB := a.Registry.SqlDB(); sqlDB != nil {
		if err := sqlDB.Close(); err != nil {
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

// reconnectLoop periodically attempts to reconnect failed services.
func (a *App) reconnectLoop(ctx context.Context) {
	ticker := time.NewTicker(a.Config.ReconnectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.tryReconnect(ctx) {
				return
			}
		}
	}
}

// tryReconnect attempts to connect any missing services. Returns true when
// all services are connected and the reconnection loop can stop.
func (a *App) tryReconnect(ctx context.Context) bool {
	reg := a.Registry

	if reg.DB() == nil {
		a.Logger.Info("Attempting to reconnect to database...")
		if err := a.initDB(); err != nil {
			a.Logger.Warn("DB reconnection failed", zap.Error(err))
		} else {
			a.Logger.Info("DB reconnection successful")
			a.initDBDependents(a.workerCtx, a.Cache)
		}
	}

	if reg.S3() == nil {
		a.Logger.Info("Attempting to reconnect to S3...")
		if err := a.initS3(); err != nil {
			a.Logger.Warn("S3 reconnection failed", zap.Error(err))
		} else {
			a.Logger.Info("S3 reconnection successful")
			// If MCP was waiting on S3, try to initialize it now
			if a.Config.MCPEnabled && reg.MCPHandler() == nil && reg.Manager() != nil && reg.DB() != nil {
				mcpSvc := veloriamc.NewDirectService(reg.Manager(), reg.DB(), reg.S3())
				mcpServer := veloriamc.NewMCPServer(a.Config.Name, config.Version, mcpSvc)
				reg.SetMCPHandler(veloriamc.NewHTTPHandler(mcpServer))
				a.Logger.Info("MCP server enabled after S3 reconnection")
			}
		}
	}

	return reg.DB() != nil && reg.S3() != nil
}

func (a *App) initDB() error {
	c := a.Config

	var logLevel gormlogger.LogLevel
	if c.AppDebug {
		logLevel = gormlogger.Info
	} else {
		logLevel = gormlogger.Error
	}

	dbLogger := gormlogger.New(zap.NewStdLog(a.Logger.Named("gorm")), gormlogger.Config{
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

	a.Registry.SetDB(db, sqlDB)
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

	a.Registry.SetS3(s3)
	return nil
}

// controlSocketPath returns the path for the Unix control socket.
func (a *App) controlSocketPath() string {
	return filepath.Join(a.Config.DataDir, "veloria.sock")
}

// ctlRequest is the JSON payload for control socket commands.
type ctlRequest struct {
	Action   string `json:"action"`
	Enabled  bool   `json:"enabled,omitempty"`
	RepoType string `json:"repo_type,omitempty"`
	Slug     string `json:"slug,omitempty"`
}

// ctlResponse is the JSON response from control socket commands.
type ctlResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func (a *App) startControlSocket() error {
	sockPath := a.controlSocketPath()
	// Remove stale socket file from previous run
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("failed to listen on control socket %s: %w", sockPath, err)
	}
	a.ctlListener = ln

	go a.serveControlSocket(ln)
	a.Logger.Info("Control socket listening", zap.String("path", sockPath))
	return nil
}

func (a *App) serveControlSocket(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed
		}
		go a.handleControlConn(conn)
	}
}

func (a *App) handleControlConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var req ctlRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(ctlResponse{Message: "invalid request"})
		return
	}

	var resp ctlResponse
	switch req.Action {
	case "maintenance":
		a.Registry.SetMaintenance(req.Enabled)
		state := "off"
		if req.Enabled {
			state = "on"
		}
		resp = ctlResponse{OK: true, Message: fmt.Sprintf("Maintenance mode %s", state)}
		a.Logger.Info("Maintenance mode changed via CLI", zap.Bool("enabled", req.Enabled))
	case "reindex":
		mgr := a.Registry.Manager()
		if mgr == nil {
			resp = ctlResponse{Message: "indexer unavailable"}
			break
		}
		if err := mgr.SubmitReindex(req.RepoType, req.Slug); err != nil {
			resp = ctlResponse{Message: err.Error()}
			break
		}
		resp = ctlResponse{OK: true, Message: fmt.Sprintf("Queued %s/%s for re-index", req.RepoType, req.Slug)}
		a.Logger.Info("Re-index queued via CLI", zap.String("repo", req.RepoType), zap.String("slug", req.Slug))
	default:
		resp = ctlResponse{Message: fmt.Sprintf("unknown action: %s", req.Action)}
	}

	_ = json.NewEncoder(conn).Encode(resp)
}
