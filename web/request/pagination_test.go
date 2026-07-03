package request_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/request"
)

func TestQueryPageDefaults(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	p, err := request.QueryPage(r)
	require.NoError(t, err)
	assert.Equal(t, 1, p.Number)
	assert.Equal(t, 20, p.Size)
	assert.Equal(t, 0, p.Offset)
}

func TestQueryPageValues(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?page=3&per_page=10", nil)
	p, err := request.QueryPage(r)
	require.NoError(t, err)
	assert.Equal(t, 3, p.Number)
	assert.Equal(t, 10, p.Size)
	assert.Equal(t, 20, p.Offset)
}

func TestQueryPageClamp(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?page=0&per_page=9999", nil)
	p, err := request.QueryPage(r, request.WithMaxPageSize(100))
	require.NoError(t, err)
	assert.Equal(t, 1, p.Number) // clamped up to 1
	assert.Equal(t, 100, p.Size) // clamped down to max
	assert.Equal(t, 0, p.Offset) // (1-1)*100
}

func TestQueryPageMalformed(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?page=abc", nil)
	_, err := request.QueryPage(r)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestQueryPageCustomParams(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?p=2&size=5", nil)
	p, err := request.QueryPage(r, request.WithPageParams("p", "size"))
	require.NoError(t, err)
	assert.Equal(t, 2, p.Number)
	assert.Equal(t, 5, p.Size)
}

func TestQueryCursor(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?cursor=abc123&limit=5", nil)
	c, err := request.QueryCursor(r)
	require.NoError(t, err)
	assert.Equal(t, "abc123", c.Value)
	assert.Equal(t, 5, c.Limit)
}

func TestQueryCursorDefaults(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	c, err := request.QueryCursor(r)
	require.NoError(t, err)
	assert.Equal(t, "", c.Value)
	assert.Equal(t, 20, c.Limit)
}

func TestQueryCursorMalformedLimit(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?limit=abc", nil)
	_, err := request.QueryCursor(r)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestQueryCursorLimitClamp(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?limit=9999", nil)
	c, err := request.QueryCursor(r, request.WithMaxPageSize(100))
	require.NoError(t, err)
	assert.Equal(t, 100, c.Limit)
}

func TestQueryCursorCustomParams(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?c=tok&n=5", nil)
	c, err := request.QueryCursor(r, request.WithCursorParams("c", "n"))
	require.NoError(t, err)
	assert.Equal(t, "tok", c.Value)
	assert.Equal(t, 5, c.Limit)
}
