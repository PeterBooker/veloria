package search

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "veloria/internal/api"
	"veloria/internal/service"
)

func TestViewSearchV1_NilDB(t *testing.T) {
	reg := &service.Registry{}
	handler := ViewSearchV1(reg)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/search/123", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var body api.APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "searches are unavailable", body.Message)
}

func TestCreateSearchV1_NilDeps(t *testing.T) {
	reg := &service.Registry{}
	handler := CreateSearchV1(reg)
	body := `{"term": "test", "repo": "plugins"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestListSearchesV1_NilDB(t *testing.T) {
	reg := &service.Registry{}
	handler := ListSearchesV1(reg)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/searches", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
