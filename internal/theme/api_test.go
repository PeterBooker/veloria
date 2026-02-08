package theme

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "veloria/internal/api"
)

func TestViewThemeV1_NilDB(t *testing.T) {
	handler := ViewThemeV1(nil)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/theme/123", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var body api.APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "themes are unavailable", body.Message)
}

func TestListThemesV1_NilDB(t *testing.T) {
	handler := ListThemesV1(nil, nil)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/themes", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
