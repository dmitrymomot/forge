package request_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/request"
)

func formReq(values url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestFormValue(t *testing.T) {
	t.Parallel()
	r := formReq(url.Values{"age": {"30"}})

	v, err := request.FormValue[int](r, "age")
	require.NoError(t, err)
	assert.Equal(t, 30, v)
	assert.True(t, request.HasForm(r, "age"))
	assert.False(t, request.HasForm(r, "missing"))
}

func TestFormSlice(t *testing.T) {
	t.Parallel()
	r := formReq(url.Values{"tag": {"a", "b"}})

	v, err := request.FormSlice[string](r, "tag")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, v)
}

func TestFormSplit(t *testing.T) {
	t.Parallel()
	r := formReq(url.Values{"ids": {"1, 2 ,3"}})

	v, err := request.FormSplit[int](r, "ids", ",")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, v)
}

func TestFormNotMergedWithQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/?age=99",
		strings.NewReader(url.Values{"age": {"30"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	v, err := request.FormValue[int](r, "age")
	require.NoError(t, err)
	assert.Equal(t, 30, v) // body value, not the query's 99
}
