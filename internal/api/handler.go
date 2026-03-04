package api

import (
	"context"
	"net/http"
)

// EndpointFunc is the business-logic function for a request. Errors implementing
// StatusCoder will have their status code used automatically by WriteError.
type EndpointFunc[Req, Resp any] func(ctx context.Context, req Req) (Resp, error)

// DecodeFunc extracts a typed request from an HTTP request.
// Returning an error writes an error response (400 if the error is an *APIError
// with that status, or whatever StatusCoder dictates).
type DecodeFunc[Req any] func(r *http.Request) (Req, error)

// Handler is a generic HTTP handler inspired by Go-Kit's transport/http.Server.
// It cleanly separates decoding, business logic, and encoding into distinct phases.
type Handler[Req, Resp any] struct {
	Decode   DecodeFunc[Req]
	Endpoint EndpointFunc[Req, Resp]
	Status   int // HTTP status on success; defaults to 200
}

// ServeHTTP implements http.Handler.
func (h Handler[Req, Resp]) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := h.Decode(r)
	if err != nil {
		WriteError(w, err)
		return
	}

	resp, err := h.Endpoint(r.Context(), req)
	if err != nil {
		WriteError(w, err)
		return
	}

	status := h.Status
	if status == 0 {
		status = http.StatusOK
	}
	WriteSuccessJSON(w, status, resp)
}
