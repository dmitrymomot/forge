package tenant_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/tenant"
)

// captureTenant records the tenant seen by the wrapped handler.
func captureTenant(id *string, ok *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*id, *ok = tenant.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("first non-empty resolver wins", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.Middleware(
			tenant.Header("X-Missing"),
			tenant.Header("X-First"),
			tenant.Header("X-Second"),
		)(captureTenant(&id, &ok))

		r := newRequest("example.com", "/")
		r.Header.Set("X-First", "alpha")
		r.Header.Set("X-Second", "beta")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		require.True(t, ok)
		assert.Equal(t, "alpha", id)
	})

	t.Run("unresolved passes through untenanted", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.Middleware(tenant.Header("X-Tenant-ID"))(captureTenant(&id, &ok))

		w := httptest.NewRecorder()
		h.ServeHTTP(w, newRequest("example.com", "/"))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, ok)
	})

	t.Run("resolver error responds 500 and skips next", func(t *testing.T) {
		t.Parallel()
		called := false
		boom := errors.New("lookup down")
		h := tenant.Middleware(func(*http.Request) (string, error) { return "", boom })(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		w := httptest.NewRecorder()
		h.ServeHTTP(w, newRequest("example.com", "/"))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.False(t, called)
	})

	t.Run("resolution overrides pre-existing context tenant", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.Middleware(tenant.Header("X-Tenant-ID"))(captureTenant(&id, &ok))

		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "upstream"))
		r.Header.Set("X-Tenant-ID", "resolved")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		require.True(t, ok)
		assert.Equal(t, "resolved", id)
	})

	t.Run("Context resolver slots upstream tenant into precedence", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.Middleware(tenant.Context(), tenant.Header("X-Tenant-ID"))(captureTenant(&id, &ok))

		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "upstream"))
		r.Header.Set("X-Tenant-ID", "header")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		require.True(t, ok)
		assert.Equal(t, "upstream", id)
	})

	t.Run("upstream tenant survives when nothing resolves", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.Middleware(tenant.Header("X-Tenant-ID"))(captureTenant(&id, &ok))

		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "upstream"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		require.True(t, ok)
		assert.Equal(t, "upstream", id)
	})

	t.Run("no resolvers is identity-ish", func(t *testing.T) {
		t.Parallel()
		var ok bool
		var id string
		h := tenant.Middleware()(captureTenant(&id, &ok))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, newRequest("example.com", "/"))
		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, ok)
	})

	t.Run("nil resolver panics at construction", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilResolver, func() {
			tenant.Middleware(tenant.Context(), nil)
		})
	})
}

func TestRequire(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("tenanted request passes", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "acme"))
		w := httptest.NewRecorder()
		tenant.Require(next).ServeHTTP(w, r)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("untenanted request gets 404", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		tenant.Require(next).ServeHTTP(w, newRequest("example.com", "/"))
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
