package api

import (
	"encoding/json"
	"errors"
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

// MaxJSONBodyBytes is the largest request body DecodeJSON reads. It is the
// same 1 MiB limit the form handlers apply.
const MaxJSONBodyBytes = 1 << 20

// DecodeJSON decodes a JSON request body into the target type.
// Unknown fields are rejected. A body over MaxJSONBodyBytes is not read past
// the limit and yields a 413 error.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var v T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return v, ErrTooLarge("request body too large")
		}
		return v, ErrBadRequest("failed to decode JSON body")
	}
	return v, nil
}
