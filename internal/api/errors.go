package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

// APIError is a structured error returned by API handlers.
type APIError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
	Detail  string `json:"detail,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}

// ErrBadRequest creates a 400 error.
func ErrBadRequest(msg string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Message: msg}
}

// ErrBadRequestf creates a 400 error with detail.
func ErrBadRequestf(msg, detail string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Message: msg, Detail: detail}
}

// ErrNotFound creates a 404 error.
func ErrNotFound(msg string) *APIError {
	return &APIError{Status: http.StatusNotFound, Message: msg}
}

// ErrInternal creates a 500 error.
func ErrInternal(msg string) *APIError {
	return &APIError{Status: http.StatusInternalServerError, Message: msg}
}

// ErrUnavailable creates a 503 error.
func ErrUnavailable(msg string) *APIError {
	return &APIError{Status: http.StatusServiceUnavailable, Message: msg}
}

// ErrTimeout creates a 408 error.
func ErrTimeout(msg string) *APIError {
	return &APIError{Status: http.StatusRequestTimeout, Message: msg}
}

// ParseID parses a UUID or ULID string into a uuid.UUID.
// If the input is a valid UUID it is returned directly; otherwise it is
// parsed as a ULID and the same 128 bits are reinterpreted as a UUID.
func ParseID(s string) (uuid.UUID, error) {
	if id, err := uuid.Parse(s); err == nil {
		return id, nil
	}
	ulidVal, err := ulid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid id %q: not a valid UUID or ULID", s)
	}
	return uuid.UUID(ulidVal), nil
}

// WriteJSON writes an APIError as a JSON response.
func WriteJSON(w http.ResponseWriter, err *APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Status)
	_ = json.NewEncoder(w).Encode(err)
}

// WriteSuccessJSON writes a success JSON response with the given status code.
func WriteSuccessJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
