package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"veloria/internal/mcp"
	"veloria/internal/service"
)

func TestMCPRequestBodyLimit(t *testing.T) {
	// The size the other POST routes allow.
	const limit = 1 << 20

	// pingBody returns a valid JSON-RPC ping of exactly size bytes.
	pingBody := func(size int) string {
		const head, tail = `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`, `"}}`
		return head + strings.Repeat("A", size-len(head)-len(tail)) + tail
	}

	for _, rateLimit := range []bool{false, true} {
		reg := &service.Registry{}
		reg.SetMCPHandler(mcp.NewHTTPHandler(mcp.NewMCPServer("veloria-test", "0", nil)))
		h := New(RouterDeps{
			Logger:   zap.NewNop(),
			Registry: reg,
			Options:  Options{MCPEnabled: true, RateLimitEnabled: rateLimit},
		})

		tests := []struct {
			name   string
			size   int
			wantOK bool
		}{
			{"small body", 256, true},
			{"body at the limit", limit, true},
			{"body one byte over the limit", limit + 1, false},
			{"body of twice the limit", 2 * limit, false},
		}

		for _, tt := range tests {
			name := tt.name
			if rateLimit {
				name += " with rate limiting"
			}
			t.Run(name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(pingBody(tt.size)))
				req.Header.Set("Content-Type", "application/json")
				// Ignored by a stateless transport; a well-formed ID keeps
				// this test about the body limit whatever the session mode.
				req.Header.Set("Mcp-Session-Id", "mcp-session-00000000-0000-0000-0000-000000000000")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)

				if tt.wantOK {
					if w.Code != http.StatusOK {
						t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body)
					}
					return
				}
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "request body too large") {
					t.Errorf("status = %d, want %d with a body-too-large error; body = %s", w.Code, http.StatusBadRequest, w.Body)
				}
			})
		}
	}
}
