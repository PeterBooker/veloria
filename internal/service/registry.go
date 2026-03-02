package service

import (
	"database/sql"
	"net/http"
	"sync"

	"gorm.io/gorm"

	"veloria/internal/auth"
	"veloria/internal/manager"
	"veloria/internal/repo"
	"veloria/internal/storage"
	"veloria/internal/tasks"
)

// Registry is a thread-safe container for mutable service references.
// It allows handlers to resolve dependencies at request time rather than
// at route-registration time, enabling dynamic reconnection after startup.
type Registry struct {
	mu          sync.RWMutex
	db          *gorm.DB
	sqlDB       *sql.DB
	s3          storage.ResultStorage
	manager     *manager.Manager
	tasks       *tasks.Tasks
	apiClient   *repo.APIClient
	session     *auth.SessionStore
	authHandler *auth.Handler
	mcpHandler  http.Handler
	maintenance bool
}

func (r *Registry) DB() *gorm.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

func (r *Registry) SetDB(db *gorm.DB, sqlDB *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
	r.sqlDB = sqlDB
}

func (r *Registry) SqlDB() *sql.DB {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sqlDB
}

func (r *Registry) S3() storage.ResultStorage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.s3
}

func (r *Registry) SetS3(s3 storage.ResultStorage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.s3 = s3
}

func (r *Registry) Manager() *manager.Manager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.manager
}

func (r *Registry) SetManager(m *manager.Manager) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.manager = m
}

func (r *Registry) Tasks() *tasks.Tasks {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tasks
}

func (r *Registry) SetTasks(t *tasks.Tasks) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks = t
}

func (r *Registry) APIClient() *repo.APIClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.apiClient
}

func (r *Registry) SetAPIClient(c *repo.APIClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.apiClient = c
}

func (r *Registry) Session() *auth.SessionStore {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.session
}

func (r *Registry) SetSession(s *auth.SessionStore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.session = s
}

func (r *Registry) Auth() *auth.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.authHandler
}

func (r *Registry) SetAuth(h *auth.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.authHandler = h
}

func (r *Registry) MCPHandler() http.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mcpHandler
}

func (r *Registry) SetMCPHandler(h http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mcpHandler = h
}

// SearchEnabled returns true when all search prerequisites are met and
// the system is not in maintenance mode.
func (r *Registry) SearchEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.maintenance && r.db != nil && r.s3 != nil && r.manager != nil
}

// SearchDisabledReason returns a human-readable reason why search is
// unavailable, or an empty string if search is enabled.
func (r *Registry) SearchDisabledReason() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.maintenance {
		return "Service is undergoing maintenance."
	}
	switch {
	case r.db == nil:
		return "Database connection is unavailable."
	case r.s3 == nil:
		return "Search storage is unavailable."
	case r.manager == nil:
		return "Search index is not ready."
	default:
		return ""
	}
}

func (r *Registry) InMaintenance() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.maintenance
}

func (r *Registry) SetMaintenance(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maintenance = enabled
}
