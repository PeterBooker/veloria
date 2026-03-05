package api

import (
	"context"
	"net/http"

	"veloria/internal/service"
)

// ListConfig configures a generic paginated list handler.
type ListConfig[T any] struct {
	// EntityName is used in error messages (e.g., "plugins", "themes").
	EntityName string

	// Table is the database table name.
	Table string

	// SelectColumns are the columns to select (e.g., "id, name, slug, version, updated_at").
	SelectColumns string

	// WhereClause is the base filter (e.g., "deleted_at IS NULL").
	WhereClause string

	// OrderClauses are applied in sequence (e.g., []string{"updated_at DESC", "slug ASC"}).
	OrderClauses []string

	// Enrich is an optional post-processing step (e.g., adding index status).
	// Called with the fetched items; modifies them in place.
	Enrich func(reg *service.Registry, items []T)
}

// ListHandler creates a paginated list handler from configuration.
// It handles nil-DB checks, pagination parsing, COUNT + SELECT, and optional enrichment.
func ListHandler[T any](reg *service.Registry, cfg ListConfig[T]) http.Handler {
	return Handler[Pagination, ListResponse[T]]{
		Decode: func(r *http.Request) (Pagination, error) {
			if reg.DB() == nil {
				return Pagination{}, ErrUnavailable(cfg.EntityName + " are unavailable")
			}
			p, err := ParsePagination(r)
			if err != nil {
				return Pagination{}, ErrBadRequest(err.Error())
			}
			return p, nil
		},
		Endpoint: func(_ context.Context, pg Pagination) (ListResponse[T], error) {
			db := reg.DB()
			var zero ListResponse[T]

			var total int64
			if err := db.Table(cfg.Table).Where(cfg.WhereClause).Count(&total).Error; err != nil {
				return zero, ErrInternal("error counting " + cfg.EntityName)
			}

			var items []T
			q := db.Table(cfg.Table).
				Select(cfg.SelectColumns).
				Where(cfg.WhereClause)
			for _, order := range cfg.OrderClauses {
				q = q.Order(order)
			}
			if err := q.Limit(pg.Limit).Offset(pg.Offset).Scan(&items).Error; err != nil {
				return zero, ErrInternal("error fetching " + cfg.EntityName)
			}

			if cfg.Enrich != nil {
				cfg.Enrich(reg, items)
			}

			return ListResponse[T]{
				Page:    pg.Page,
				PerPage: pg.PerPage,
				Total:   total,
				Results: items,
			}, nil
		},
	}
}
