package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

// StatusCoder is implemented by errors that know their HTTP status code.
// Inspired by Go-Kit's transport pattern: domain errors carry transport metadata
// so callers don't need manual error-type switches.
type StatusCoder interface {
	StatusCode() int
}

// APIError is a structured error returned by API handlers.
type APIError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
	Detail  string `json:"detail,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}

// StatusCode implements the StatusCoder interface.
func (e *APIError) StatusCode() int {
	return e.Status
}

// ErrBadRequest creates a 400 error.
func ErrBadRequest(msg string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Message: msg}
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

// WriteError writes any error as a JSON response. It checks for *APIError first,
// then falls back to the StatusCoder interface for the HTTP status code.
// Plain errors without StatusCoder produce a 500 with a generic message.
func WriteError(w http.ResponseWriter, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		WriteJSON(w, apiErr)
		return
	}

	status := http.StatusInternalServerError
	if sc, ok := err.(StatusCoder); ok {
		status = sc.StatusCode()
	}

	msg := "internal server error"
	if status != http.StatusInternalServerError {
		msg = err.Error()
	}

	WriteJSON(w, &APIError{Status: status, Message: msg})
}
