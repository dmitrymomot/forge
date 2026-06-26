package request_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/request"
)

type payload struct {
	Name string `json:"name"`
}

func jsonReq(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestDecodeJSONHappy(t *testing.T) {
	t.Parallel()
	var p payload
	require.NoError(t, request.DecodeJSON(jsonReq(`{"name":"ada"}`), &p))
	assert.Equal(t, "ada", p.Name)
}

func TestDecodeJSONUnknownField(t *testing.T) {
	t.Parallel()
	var p payload
	err := request.DecodeJSON(jsonReq(`{"name":"ada","extra":1}`), &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestDecodeJSONAllowUnknown(t *testing.T) {
	t.Parallel()
	var p payload
	require.NoError(t, request.DecodeJSON(jsonReq(`{"name":"ada","extra":1}`), &p, request.AllowUnknownFields()))
	assert.Equal(t, "ada", p.Name)
}

func TestDecodeJSONWrongContentType(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "text/plain")
	var p payload
	err := request.DecodeJSON(r, &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, request.StatusCode(err))
}

func TestDecodeJSONSkipContentType(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
	// no Content-Type set
	var p payload
	require.NoError(t, request.DecodeJSON(r, &p, request.SkipContentType()))
	assert.Equal(t, "x", p.Name)
}

func TestDecodeJSONTooLarge(t *testing.T) {
	t.Parallel()
	var p payload
	big := `{"name":"` + strings.Repeat("x", 2000) + `"}`
	err := request.DecodeJSON(jsonReq(big), &p, request.WithMaxBytes(100))
	require.Error(t, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, request.StatusCode(err))
}

func TestDecodeJSONTrailingData(t *testing.T) {
	t.Parallel()
	var p payload
	err := request.DecodeJSON(jsonReq(`{"name":"a"}{"name":"b"}`), &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestDecodeJSONEmpty(t *testing.T) {
	t.Parallel()
	var p payload
	err := request.DecodeJSON(jsonReq(``), &p)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestIsContentType(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Content-Type", "Application/JSON; charset=utf-8")
	assert.True(t, request.IsContentType(r, "application/json"))
	assert.False(t, request.IsContentType(r, "text/plain"))

	bare := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.False(t, request.IsContentType(bare, "application/json"))
}
