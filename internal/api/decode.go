package api

import (
	"encoding/json"
	"errors"
	"io"
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
	body := &limitedBody{r: http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes)}
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		if body.tooLarge {
			return v, ErrTooLarge("request body too large")
		}
		return v, ErrBadRequest("failed to decode JSON body")
	}
	return v, nil
}

// limitedBody records whether the size limit was hit while reading. The
// decoder does not reliably keep http.MaxBytesError in the error it returns
// (it is lost under GOEXPERIMENT=jsonv2), so the limit is observed here.
type limitedBody struct {
	r        io.Reader
	tooLarge bool
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			b.tooLarge = true
		}
	}
	return n, err
}
