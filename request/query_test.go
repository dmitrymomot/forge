package request_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// color is a local TextUnmarshaler type — proves custom types parse with no
// external dependency and no reflection.
type color struct{ name string }

func (c *color) UnmarshalText(b []byte) error {
	switch s := string(b); s {
	case "red", "green", "blue":
		c.name = s
		return nil
	default:
		return fmt.Errorf("bad color %q", s)
	}
}

func TestQueryScalars(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?n=42&ok=true&f=3.5&s=hi&d=1500ms", nil)

	n, err := request.Query[int](r, "n")
	require.NoError(t, err)
	assert.Equal(t, 42, n)

	ok, err := request.Query[bool](r, "ok")
	require.NoError(t, err)
	assert.True(t, ok)

	f, err := request.Query[float64](r, "f")
	require.NoError(t, err)
	assert.InEpsilon(t, 3.5, f, 1e-9)

	s, err := request.Query[string](r, "s")
	require.NoError(t, err)
	assert.Equal(t, "hi", s)

	d, err := request.Query[time.Duration](r, "d")
	require.NoError(t, err)
	assert.Equal(t, 1500*time.Millisecond, d)
}

func TestQueryTextUnmarshaler(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?c=blue", nil)
	c, err := request.Query[color](r, "c")
	require.NoError(t, err)
	assert.Equal(t, "blue", c.name)
}

func TestQueryMalformed(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?n=oops", nil)

	_, err := request.Query[int](r, "n")
	require.Error(t, err)

	var re *request.Error
	require.ErrorAs(t, err, &re)
	assert.Equal(t, request.SourceQuery, re.Source)
	assert.Equal(t, request.KindMalformed, re.Kind)
	assert.Equal(t, "n", re.Key)
}

func TestQueryAbsentAndDefault(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	n, err := request.Query[int](r, "n")
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	n2, err := request.Query[int](r, "n", 7)
	require.NoError(t, err)
	assert.Equal(t, 7, n2)
}

func TestQueryFunc(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?hex=ff", nil)
	v, err := request.QueryFunc(r, "hex", func(s string) (int64, error) {
		return strconv.ParseInt(s, 16, 64)
	})
	require.NoError(t, err)
	assert.Equal(t, int64(255), v)
}

func TestQuerySlice(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?id=1&id=2&id=3", nil)
	ids, err := request.QuerySlice[int](r, "id")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, ids)
}

func TestQuerySplit(t *testing.T) {
	t.Parallel()
	// decodes to: filter=orange, blue ,gray,
	r := httptest.NewRequest(http.MethodGet, "/?filter=orange,%20blue%20,gray,", nil)
	got, err := request.QuerySplit[string](r, "filter", ",")
	require.NoError(t, err)
	assert.Equal(t, []string{"orange", "blue", "gray"}, got)
}

func TestHasQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?x=", nil)
	assert.True(t, request.HasQuery(r, "x"))
	assert.False(t, request.HasQuery(r, "y"))
}
