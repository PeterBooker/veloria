package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestDecodeUUIDParam_Valid(t *testing.T) {
	expected := uuid.New()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", expected.String())

	id, err := DecodeUUIDParam(r, "id")
	require.NoError(t, err)
	assert.Equal(t, expected, id)
}

func TestDecodeUUIDParam_Invalid(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "not-a-uuid")

	_, err := DecodeUUIDParam(r, "id")
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, err.(*APIError).Status)
}

func TestDecodeIDParam_UUID(t *testing.T) {
	expected := uuid.New()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", expected.String())

	id, err := DecodeIDParam(r, "id")
	require.NoError(t, err)
	assert.Equal(t, expected, id)
}

func TestDecodeIDParam_Invalid(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withChiParam(r, "id", "!!!invalid!!!")

	_, err := DecodeIDParam(r, "id")
	require.Error(t, err)
}

func TestDecodeJSON_Valid(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	body := `{"name":"test"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	v, err := DecodeJSON[req](httptest.NewRecorder(), r)
	require.NoError(t, err)
	assert.Equal(t, "test", v.Name)
}

func TestDecodeJSON_InvalidJSON(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{bad"))

	_, err := DecodeJSON[req](httptest.NewRecorder(), r)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, err.(*APIError).Status)
}

func TestDecodeJSON_UnknownFields(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	body := `{"name":"test","extra":"field"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	_, err := DecodeJSON[req](httptest.NewRecorder(), r)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, err.(*APIError).Status)
}

// countingReader records how many bytes were read from it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// A body over MaxJSONBodyBytes must be refused with 413 without the server
// buffering the rest of it.
func TestDecodeJSON_BodyTooLarge(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	body := &countingReader{r: strings.NewReader(`{"name":"` + strings.Repeat("a", 4*MaxJSONBodyBytes) + `"}`)}
	r := httptest.NewRequest(http.MethodPost, "/", body)

	_, err := DecodeJSON[req](httptest.NewRecorder(), r)
	require.Error(t, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, err.(*APIError).Status)
	assert.LessOrEqual(t, body.n, MaxJSONBodyBytes+1, "bytes read from an oversized body")
}

func TestDecodeJSON_BodyAtLimit(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	const wrapper = len(`{"name":""}`)
	name := strings.Repeat("a", MaxJSONBodyBytes-wrapper)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"`+name+`"}`))

	v, err := DecodeJSON[req](httptest.NewRecorder(), r)
	require.NoError(t, err)
	assert.Len(t, v.Name, len(name))
}
