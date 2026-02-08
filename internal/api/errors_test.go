package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrBadRequest(t *testing.T) {
	err := ErrBadRequest("invalid input")
	assert.Equal(t, http.StatusBadRequest, err.Status)
	assert.Equal(t, "invalid input", err.Message)
	assert.Equal(t, "invalid input", err.Error())
}

func TestErrNotFound(t *testing.T) {
	err := ErrNotFound("resource not found")
	assert.Equal(t, http.StatusNotFound, err.Status)
	assert.Equal(t, "resource not found", err.Message)
}

func TestErrInternal(t *testing.T) {
	err := ErrInternal("something went wrong")
	assert.Equal(t, http.StatusInternalServerError, err.Status)
}

func TestErrUnavailable(t *testing.T) {
	err := ErrUnavailable("service down")
	assert.Equal(t, http.StatusServiceUnavailable, err.Status)
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, ErrNotFound("not found"))

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "not found", body.Message)
}

func TestWriteSuccessJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	WriteSuccessJSON(w, http.StatusCreated, data)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "value", body["key"])
}

func TestWriteJSON_WithDetail(t *testing.T) {
	w := httptest.NewRecorder()
	err := &APIError{
		Status:  http.StatusBadRequest,
		Message: "validation failed",
		Detail:  "field 'name' is required",
	}
	WriteJSON(w, err)

	var body APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "validation failed", body.Message)
	assert.Equal(t, "field 'name' is required", body.Detail)
}
