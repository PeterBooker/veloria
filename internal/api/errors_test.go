package api

import (
	"encoding/json"
	"fmt"
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

func TestAPIError_ImplementsStatusCoder(t *testing.T) {
	err := ErrNotFound("gone")
	var sc StatusCoder = err
	assert.Equal(t, http.StatusNotFound, sc.StatusCode())
}

// customStatusErr is a test error implementing StatusCoder.
type customStatusErr struct {
	code int
	msg  string
}

func (e *customStatusErr) Error() string  { return e.msg }
func (e *customStatusErr) StatusCode() int { return e.code }

func TestWriteError_WithAPIError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, ErrBadRequest("bad input"))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "bad input", body.Message)
}

func TestWriteError_WithStatusCoder(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, &customStatusErr{code: http.StatusConflict, msg: "already exists"})

	assert.Equal(t, http.StatusConflict, w.Code)
	var body APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "already exists", body.Message)
}

func TestWriteError_PlainError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, fmt.Errorf("something broke"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var body APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "internal server error", body.Message)
}
