package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_Success(t *testing.T) {
	h := Handler[string, map[string]string]{
		Decode: func(r *http.Request) (string, error) {
			return "hello", nil
		},
		Endpoint: func(_ context.Context, req string) (map[string]string, error) {
			return map[string]string{"echo": req}, nil
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "hello", body["echo"])
}

func TestHandler_CustomStatus(t *testing.T) {
	h := Handler[string, map[string]string]{
		Decode: func(r *http.Request) (string, error) {
			return "", nil
		},
		Endpoint: func(_ context.Context, _ string) (map[string]string, error) {
			return map[string]string{"created": "true"}, nil
		},
		Status: http.StatusCreated,
	}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestHandler_DecodeError(t *testing.T) {
	h := Handler[string, string]{
		Decode: func(r *http.Request) (string, error) {
			return "", ErrBadRequest("bad input")
		},
		Endpoint: func(_ context.Context, _ string) (string, error) {
			t.Fatal("endpoint should not be called on decode error")
			return "", nil
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "bad input", body.Message)
}

func TestHandler_EndpointStatusCoderError(t *testing.T) {
	h := Handler[string, string]{
		Decode: func(r *http.Request) (string, error) {
			return "ok", nil
		},
		Endpoint: func(_ context.Context, _ string) (string, error) {
			return "", ErrNotFound("not here")
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_EndpointPlainError(t *testing.T) {
	h := Handler[string, string]{
		Decode: func(r *http.Request) (string, error) {
			return "ok", nil
		},
		Endpoint: func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("unexpected failure")
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var body APIError
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "internal server error", body.Message)
}
