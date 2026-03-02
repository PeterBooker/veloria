package plugin

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

func TestViewPluginV1_NilDB(t *testing.T) {
	reg := &service.Registry{}
	handler := ViewPluginV1(reg)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/plugin/123", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var body api.APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "plugins are unavailable", body.Message)
}

func TestListPluginsV1_NilDB(t *testing.T) {
	reg := &service.Registry{}
	handler := ListPluginsV1(reg)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
