package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"veloria/internal/manager"
)

// Status represents the overall health state.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"

	dbPingTimeout = 3 * time.Second
)

// ComponentStatus describes the health of a single component.
type ComponentStatus struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Response is the JSON body returned by the /health endpoint.
type Response struct {
	Status     Status                     `json:"status"`
	Components map[string]ComponentStatus `json:"components"`
	Sources    map[string]SourceInfo      `json:"sources,omitempty"`
}

// SourceInfo describes the health of a data source.
type SourceInfo struct {
	Total               int       `json:"total"`
	Indexed             int       `json:"indexed"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastSuccess         time.Time `json:"last_success,omitempty"`
}

// Checker holds the dependencies needed to evaluate system health.
type Checker struct {
	DB      *sql.DB
	Manager *manager.Manager
}

// Check evaluates all components and returns a health response.
func (c *Checker) Check() *Response {
	resp := &Response{
		Status:     StatusHealthy,
		Components: make(map[string]ComponentStatus),
	}

	// Database connectivity.
	if c.DB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), dbPingTimeout)
		defer cancel()
		if err := c.DB.PingContext(ctx); err != nil {
			resp.Components["database"] = ComponentStatus{Status: StatusUnhealthy, Message: err.Error()}
			resp.Status = StatusUnhealthy
		} else {
			resp.Components["database"] = ComponentStatus{Status: StatusHealthy}
		}
	} else {
		resp.Components["database"] = ComponentStatus{Status: StatusUnhealthy, Message: "not configured"}
		resp.Status = StatusUnhealthy
	}

	// Manager and data sources.
	if c.Manager != nil {
		// Circuit breaker.
		state := c.Manager.BreakerState()
		if state == "open" {
			resp.Components["circuit_breaker"] = ComponentStatus{Status: StatusDegraded, Message: "circuit breaker is open"}
			if resp.Status == StatusHealthy {
				resp.Status = StatusDegraded
			}
		} else {
			resp.Components["circuit_breaker"] = ComponentStatus{Status: StatusHealthy, Message: state}
		}

		// Per-source health.
		sourceHealth := c.Manager.SourceHealth()
		resp.Sources = make(map[string]SourceInfo, len(sourceHealth))
		for name, sh := range sourceHealth {
			total, indexed, ok := c.Manager.Stats(name)
			info := SourceInfo{
				ConsecutiveFailures: sh.ConsecutiveFailures,
				LastSuccess:         sh.LastSuccess,
			}
			if ok {
				info.Total = total
				info.Indexed = indexed
			}
			resp.Sources[name] = info

			if sh.ConsecutiveFailures >= 3 {
				resp.Components["source_"+name] = ComponentStatus{
					Status:  StatusDegraded,
					Message: "repeated update failures",
				}
				if resp.Status == StatusHealthy {
					resp.Status = StatusDegraded
				}
			}
		}
	} else {
		resp.Components["manager"] = ComponentStatus{Status: StatusUnhealthy, Message: "not initialized"}
		resp.Status = StatusUnhealthy
	}

	return resp
}

// Handler returns an HTTP handler for the /health readiness endpoint.
func Handler(checker *Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := checker.Check()

		w.Header().Set("Content-Type", "application/json")
		if resp.Status == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(resp)
	}
}
