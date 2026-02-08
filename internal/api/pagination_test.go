package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePagination_Defaults(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	p, err := ParsePagination(r)
	require.NoError(t, err)
	assert.Equal(t, 1, p.Page)
	assert.Equal(t, DefaultPerPage, p.PerPage)
	assert.Equal(t, 0, p.Offset)
	assert.Equal(t, DefaultPerPage, p.Limit)
}

func TestParsePagination_CustomValues(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=3&per_page=25", nil)
	p, err := ParsePagination(r)
	require.NoError(t, err)
	assert.Equal(t, 3, p.Page)
	assert.Equal(t, 25, p.PerPage)
	assert.Equal(t, 50, p.Offset)
	assert.Equal(t, 25, p.Limit)
}

func TestParsePagination_MaxPerPage(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?per_page=500", nil)
	p, err := ParsePagination(r)
	require.NoError(t, err)
	assert.Equal(t, MaxPerPage, p.PerPage)
}

func TestParsePagination_InvalidPage(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=abc", nil)
	_, err := ParsePagination(r)
	assert.Error(t, err)
}

func TestParsePagination_InvalidPerPage(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?per_page=-1", nil)
	_, err := ParsePagination(r)
	assert.Error(t, err)
}

func TestParsePagination_ZeroPage(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=0", nil)
	_, err := ParsePagination(r)
	assert.Error(t, err)
}
