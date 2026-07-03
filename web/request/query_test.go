package request_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/request"
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
	require.NotNil(t, re)
	if re != nil {
		assert.Equal(t, request.SourceQuery, re.Source)
		assert.Equal(t, request.KindMalformed, re.Kind)
		assert.Equal(t, "n", re.Key)
	}
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

type noParse struct{ X int } // pointer does NOT implement encoding.TextUnmarshaler

func TestQueryScalarWidths(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet,
		"/?i8=12&i16=300&i32=70000&i64=5000000000&u=7&u8=200&u16=40000&u32=3000000000&u64=10000000000&f32=1.5", nil)

	i8, err := request.Query[int8](r, "i8")
	require.NoError(t, err)
	assert.Equal(t, int8(12), i8)
	i16, err := request.Query[int16](r, "i16")
	require.NoError(t, err)
	assert.Equal(t, int16(300), i16)
	i32, err := request.Query[int32](r, "i32")
	require.NoError(t, err)
	assert.Equal(t, int32(70000), i32)
	i64, err := request.Query[int64](r, "i64")
	require.NoError(t, err)
	assert.Equal(t, int64(5000000000), i64)
	u, err := request.Query[uint](r, "u")
	require.NoError(t, err)
	assert.Equal(t, uint(7), u)
	u8, err := request.Query[uint8](r, "u8")
	require.NoError(t, err)
	assert.Equal(t, uint8(200), u8)
	u16, err := request.Query[uint16](r, "u16")
	require.NoError(t, err)
	assert.Equal(t, uint16(40000), u16)
	u32, err := request.Query[uint32](r, "u32")
	require.NoError(t, err)
	assert.Equal(t, uint32(3000000000), u32)
	u64, err := request.Query[uint64](r, "u64")
	require.NoError(t, err)
	assert.Equal(t, uint64(10000000000), u64)
	f32, err := request.Query[float32](r, "f32")
	require.NoError(t, err)
	assert.InEpsilon(t, float32(1.5), f32, 1e-6)
}

func TestQueryUnsupportedType(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?v=anything", nil)
	_, err := request.Query[noParse](r, "v")
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}

func TestQuerySplitAllEmptyUsesDefault(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/?tags=,,", nil) // every part empty after split
	got, err := request.QuerySplit[string](r, "tags", ",", []string{"all"})
	require.NoError(t, err)
	assert.Equal(t, []string{"all"}, got)
}
