package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"veloria/internal/service"
)

// QueryFunc loads a single entity from the database by its UUID primary key.
type QueryFunc[T any] func(db *gorm.DB, id uuid.UUID) (T, error)

// ViewByID creates a handler that fetches a single record by its UUID primary key.
// entityName is used in error messages (e.g., "plugin", "theme", "core").
// idParam is the chi route parameter name (e.g., "id").
func ViewByID[T any](reg *service.Registry, entityName, idParam string, queryFn QueryFunc[T]) http.Handler {
	return Handler[uuid.UUID, T]{
		Decode: func(r *http.Request) (uuid.UUID, error) {
			if reg.DB() == nil {
				return uuid.Nil, ErrUnavailable(entityName + "s are unavailable")
			}
			return DecodeUUIDParam(r, idParam)
		},
		Endpoint: func(_ context.Context, id uuid.UUID) (T, error) {
			var zero T
			result, err := queryFn(reg.DB(), id)
			if err != nil {
				if err == gorm.ErrRecordNotFound {
					return zero, ErrNotFound(entityName + " not found")
				}
				return zero, ErrInternal("error fetching " + entityName)
			}
			return result, nil
		},
	}
}
