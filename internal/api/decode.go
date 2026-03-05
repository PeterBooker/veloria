package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// DecodeUUIDParam extracts and parses a strict UUID route parameter.
func DecodeUUIDParam(r *http.Request, param string) (uuid.UUID, error) {
	s := chi.URLParam(r, param)
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, ErrBadRequest("invalid UUID")
	}
	return id, nil
}

// DecodeIDParam extracts and parses a UUID or ULID route parameter.
func DecodeIDParam(r *http.Request, param string) (uuid.UUID, error) {
	s := chi.URLParam(r, param)
	id, err := ParseID(s)
	if err != nil {
		return uuid.Nil, ErrBadRequest("invalid id")
	}
	return id, nil
}

// DecodeJSON decodes a JSON request body into the target type.
// Unknown fields are rejected.
func DecodeJSON[T any](r *http.Request) (T, error) {
	var v T
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, ErrBadRequest("failed to decode JSON body")
	}
	return v, nil
}
