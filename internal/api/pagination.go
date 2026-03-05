package api

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	DefaultPerPage = 50
	MaxPerPage     = 200
)

type Pagination struct {
	Page    int
	PerPage int
	Offset  int
	Limit   int
}

type ListResponse[T any] struct {
	Page    int   `json:"page"`
	PerPage int   `json:"per_page"`
	Total   int64 `json:"total"`
	Results []T   `json:"results"`
}

func ParsePagination(r *http.Request) (Pagination, error) {
	page := 1
	perPage := DefaultPerPage

	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		parsed, err := strconv.Atoi(pageStr)
		if err != nil || parsed < 1 {
			return Pagination{}, fmt.Errorf("invalid page")
		}
		page = parsed
	}

	if perPageStr := r.URL.Query().Get("per_page"); perPageStr != "" {
		parsed, err := strconv.Atoi(perPageStr)
		if err != nil || parsed < 1 {
			return Pagination{}, fmt.Errorf("invalid per_page")
		}
		perPage = parsed
	}

	if perPage > MaxPerPage {
		perPage = MaxPerPage
	}

	offset := (page - 1) * perPage
	return Pagination{
		Page:    page,
		PerPage: perPage,
		Offset:  offset,
		Limit:   perPage,
	}, nil
}
