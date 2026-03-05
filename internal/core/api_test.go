package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "veloria/internal/api"
	"veloria/internal/service"
)

func TestViewCoreV1_NilDB(t *testing.T) {
	reg := &service.Registry{}
	handler := ViewCoreV1(reg)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/core/123", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var body api.APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "cores are unavailable", body.Message)
}

func TestListCoresV1_NilDB(t *testing.T) {
	reg := &service.Registry{}
	handler := ListCoresV1(reg)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/cores", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
