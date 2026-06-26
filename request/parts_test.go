package request_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/request"
)

func TestPath(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/u/42", nil)
	r.SetPathValue("id", "42")

	v, err := request.Path[int](r, "id")
	require.NoError(t, err)
	assert.Equal(t, 42, v)
	assert.True(t, request.HasPath(r, "id"))
	assert.False(t, request.HasPath(r, "missing"))
}

func TestHeader(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Count", "5")

	v, err := request.Header[int](r, "X-Count")
	require.NoError(t, err)
	assert.Equal(t, 5, v)
	assert.True(t, request.HasHeader(r, "X-Count"))
	assert.False(t, request.HasHeader(r, "X-Absent"))
}

func TestBearerToken(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer abc.def")
	tok, ok := request.BearerToken(r)
	assert.True(t, ok)
	assert.Equal(t, "abc.def", tok)

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("Authorization", "Basic xxx")
	_, ok2 := request.BearerToken(r2)
	assert.False(t, ok2)

	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	_, ok3 := request.BearerToken(r3)
	assert.False(t, ok3)
}

func TestCookie(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "sid", Value: "xyz"})

	v, err := request.Cookie[string](r, "sid")
	require.NoError(t, err)
	assert.Equal(t, "xyz", v)
	assert.True(t, request.HasCookie(r, "sid"))
	assert.False(t, request.HasCookie(r, "nope"))
}

func TestBearerTokenCaseInsensitive(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "BEARER abc.def")
	tok, ok := request.BearerToken(r)
	assert.True(t, ok)
	assert.Equal(t, "abc.def", tok)
}

func TestPartFuncVariants(t *testing.T) {
	t.Parallel()
	hexParse := func(s string) (int64, error) { return strconv.ParseInt(s, 16, 64) }

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", "ff")
	r.Header.Set("X-Hex", "10")
	r.AddCookie(&http.Cookie{Name: "c", Value: "1a"})

	p, err := request.PathFunc(r, "id", hexParse)
	require.NoError(t, err)
	assert.Equal(t, int64(255), p)

	h, err := request.HeaderFunc(r, "X-Hex", hexParse)
	require.NoError(t, err)
	assert.Equal(t, int64(16), h)

	c, err := request.CookieFunc(r, "c", hexParse)
	require.NoError(t, err)
	assert.Equal(t, int64(26), c)
}

func TestHasHeaderEmptyValue(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Flag", "")
	assert.True(t, request.HasHeader(r, "X-Flag")) // present, even though empty
	assert.False(t, request.HasHeader(r, "X-Absent"))
}
