package request_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/request"
)

// withVals returns a request carrying vals as WithPathValues fallback.
func withVals(vals map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	return r.WithContext(request.WithPathValues(r.Context(), vals))
}

func TestWithPathValues_Fallback(t *testing.T) {
	r := withVals(map[string]string{"tenant": "acme", "n": "7"})

	tenant, err := request.Path[string](r, "tenant")
	require.NoError(t, err)
	assert.Equal(t, "acme", tenant)

	n, err := request.Path[int](r, "n") // typed parsing works on fallback values
	require.NoError(t, err)
	assert.Equal(t, 7, n)

	assert.True(t, request.HasPath(r, "tenant"))
	assert.False(t, request.HasPath(r, "absent"))

	missing, err := request.Path[string](r, "absent", "def")
	require.NoError(t, err)
	assert.Equal(t, "def", missing)
}

func TestWithPathValues_CurrentMuxWins(t *testing.T) {
	mux := http.NewServeMux()
	var got string
	mux.HandleFunc("GET /u/{id}", func(_ http.ResponseWriter, r *http.Request) {
		got, _ = request.Path[string](r, "id")
	})

	req := httptest.NewRequest(http.MethodGet, "/u/OWN", nil)
	req = req.WithContext(request.WithPathValues(req.Context(), map[string]string{"id": "FALLBACK"}))
	mux.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, "OWN", got)
}

func TestWithPathValues_MergeLaterWins(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	ctx := request.WithPathValues(r.Context(), map[string]string{"a": "1", "b": "1"})
	ctx = request.WithPathValues(ctx, map[string]string{"b": "2", "c": "2"})
	r = r.WithContext(ctx)

	a, _ := request.Path[string](r, "a")
	b, _ := request.Path[string](r, "b")
	c, _ := request.Path[string](r, "c")
	assert.Equal(t, "1", a, "earlier keys retained")
	assert.Equal(t, "2", b, "later call wins per key")
	assert.Equal(t, "2", c)
}

func TestWithPathValues_EmptyMapIsNoop(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	assert.Equal(t, r.Context(), request.WithPathValues(r.Context(), nil))
	assert.Equal(t, r.Context(), request.WithPathValues(r.Context(), map[string]string{}))
}

func TestWithPathValues_CopiesInput(t *testing.T) {
	vals := map[string]string{"tenant": "acme"}
	r := withVals(vals)
	vals["tenant"] = "mutated"

	tenant, err := request.Path[string](r, "tenant")
	require.NoError(t, err)
	assert.Equal(t, "acme", tenant)
}
