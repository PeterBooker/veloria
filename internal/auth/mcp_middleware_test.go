package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"
)

func TestBearerTokenMiddleware_NoHeader_PassesThrough(t *testing.T) {
	called := false
	handler := BearerTokenMiddleware(func() *gorm.DB { return nil })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("next handler should have been called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestBearerTokenMiddleware_MalformedHeader_Returns401(t *testing.T) {
	handler := BearerTokenMiddleware(func() *gorm.DB { return nil })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("next handler should not have been called")
		}),
	)

	tests := []string{
		"Basic abc123",
		"Bearer ",
		"Bearer",
		"token abc123",
	}

	for _, authHeader := range tests {
		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.Header.Set("Authorization", authHeader)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Authorization: %q → status %d, want %d", authHeader, rr.Code, http.StatusUnauthorized)
		}
	}
}

func TestBearerTokenMiddleware_NoDB_Returns503(t *testing.T) {
	handler := BearerTokenMiddleware(func() *gorm.DB { return nil })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("next handler should not have been called")
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer vel_sometoken")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}
