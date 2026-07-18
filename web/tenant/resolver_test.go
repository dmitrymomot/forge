package tenant_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/tenant"
)

// captureTenant records the tenant seen by the wrapped handler.
func captureTenant(id *string, ok *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*id, *ok = tenant.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("no sources panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNoSources, func() { tenant.New() })
	})

	t.Run("nil source panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilSource, func() {
			tenant.WithSources(tenant.Context(), nil)
		})
	})

	t.Run("nil validator panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilComponent, func() { tenant.WithValidator(nil) })
	})

	t.Run("nil error handler panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilComponent, func() { tenant.WithErrorHandler(nil) })
	})
}

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("first non-empty source wins", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(tenant.WithSources(
			tenant.Header("X-Missing"),
			tenant.Header("X-First"),
			tenant.Header("X-Second"),
		))
		r := newRequest("example.com", "/")
		r.Header.Set("X-First", "alpha")
		r.Header.Set("X-Second", "beta")
		id, err := rv.Resolve(r)
		require.NoError(t, err)
		assert.Equal(t, "alpha", id)
	})

	t.Run("repeated WithSources appends in order", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.WithSources(tenant.Header("X-First")),
			tenant.WithSources(tenant.Header("X-Second")),
		)
		r := newRequest("example.com", "/")
		r.Header.Set("X-Second", "beta")
		id, err := rv.Resolve(r)
		require.NoError(t, err)
		assert.Equal(t, "beta", id)

		r.Header.Set("X-First", "alpha")
		id, err = rv.Resolve(r)
		require.NoError(t, err)
		assert.Equal(t, "alpha", id)
	})

	t.Run("nothing resolves returns ErrNoTenant", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(tenant.WithSources(tenant.Header("X-Tenant-ID")))
		_, err := rv.Resolve(newRequest("example.com", "/"))
		require.ErrorIs(t, err, tenant.ErrNoTenant)
	})

	t.Run("source error stops chain", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("lookup down")
		rv := tenant.New(tenant.WithSources(
			func(*http.Request) (string, error) { return "", boom },
			tenant.Header("X-Tenant-ID"),
		))
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		_, err := rv.Resolve(r)
		require.ErrorIs(t, err, boom)
	})

	t.Run("validator approves resolved ID", func(t *testing.T) {
		t.Parallel()
		var got string
		rv := tenant.New(
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithValidator(tenant.ValidatorFunc(func(_ context.Context, id string) error {
				got = id
				return nil
			})),
		)
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		id, err := rv.Resolve(r)
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
		assert.Equal(t, "acme", got)
	})

	t.Run("validator rejection fails closed without trying later sources", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.WithSources(
				tenant.Header("X-Stale"),
				tenant.Header("X-Live"),
			),
			tenant.WithValidator(tenant.ValidatorFunc(func(_ context.Context, id string) error {
				if id == "stale" {
					return tenant.ErrTenantInactive
				}
				return nil
			})),
		)
		r := newRequest("example.com", "/")
		r.Header.Set("X-Stale", "stale")
		r.Header.Set("X-Live", "live")
		_, err := rv.Resolve(r)
		require.ErrorIs(t, err, tenant.ErrTenantInactive)
	})

	t.Run("validator not-found fails closed", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithValidator(tenant.ValidatorFunc(func(context.Context, string) error {
				return tenant.ErrTenantNotFound
			})),
		)
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "ghost")
		_, err := rv.Resolve(r)
		require.ErrorIs(t, err, tenant.ErrTenantNotFound)
	})

	t.Run("validator skipped when nothing resolves", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithValidator(tenant.ValidatorFunc(func(context.Context, string) error {
				t.Fatal("validator must not run without a resolved ID")
				return nil
			})),
		)
		_, err := rv.Resolve(newRequest("example.com", "/"))
		require.ErrorIs(t, err, tenant.ErrNoTenant)
	})
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("stamps resolved tenant", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.New(tenant.WithSources(tenant.Header("X-Tenant-ID"))).
			Middleware()(captureTenant(&id, &ok))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		require.True(t, ok)
		assert.Equal(t, "acme", id)
	})

	t.Run("unresolved passes through untenanted", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.New(tenant.WithSources(tenant.Header("X-Tenant-ID"))).
			Middleware()(captureTenant(&id, &ok))

		w := httptest.NewRecorder()
		h.ServeHTTP(w, newRequest("example.com", "/"))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, ok)
	})

	t.Run("source error responds 500 and skips next", func(t *testing.T) {
		t.Parallel()
		called := false
		boom := errors.New("lookup down")
		h := tenant.New(tenant.WithSources(func(*http.Request) (string, error) { return "", boom })).
			Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		w := httptest.NewRecorder()
		h.ServeHTTP(w, newRequest("example.com", "/"))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.False(t, called)
	})

	t.Run("validator rejection responds 404 and skips next", func(t *testing.T) {
		t.Parallel()
		called := false
		h := tenant.New(
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithValidator(tenant.ValidatorFunc(func(context.Context, string) error {
				return tenant.ErrTenantInactive
			})),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.False(t, called)
	})

	t.Run("custom error handler receives the resolution error", func(t *testing.T) {
		t.Parallel()
		var got error
		h := tenant.New(
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithValidator(tenant.ValidatorFunc(func(context.Context, string) error {
				return tenant.ErrTenantNotFound
			})),
			tenant.WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error) {
				got = err
				w.WriteHeader(http.StatusTeapot)
			}),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "ghost")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusTeapot, w.Code)
		require.ErrorIs(t, got, tenant.ErrTenantNotFound)
	})

	t.Run("ErrNoTenant from a source fails closed, never passes through", func(t *testing.T) {
		t.Parallel()
		called := false
		h := tenant.New(tenant.WithSources(func(*http.Request) (string, error) {
			return "", tenant.ErrNoTenant
		})).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		w := httptest.NewRecorder()
		h.ServeHTTP(w, newRequest("example.com", "/"))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.False(t, called)
	})

	t.Run("ErrNoTenant from the validator fails closed, never passes through", func(t *testing.T) {
		t.Parallel()
		called := false
		h := tenant.New(
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithValidator(tenant.ValidatorFunc(func(context.Context, string) error {
				return tenant.ErrNoTenant
			})),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.False(t, called)
	})

	t.Run("rejections log at debug, infrastructure errors at error", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		h := tenant.New(
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithValidator(tenant.ValidatorFunc(func(context.Context, string) error {
				return tenant.ErrTenantInactive
			})),
			tenant.WithLogger(log),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		h.ServeHTTP(httptest.NewRecorder(), r)
		assert.Contains(t, buf.String(), "tenant: resolution failed")
		assert.Contains(t, buf.String(), "level=DEBUG")

		buf.Reset()
		h = tenant.New(
			tenant.WithSources(func(*http.Request) (string, error) { return "", errors.New("db down") }),
			tenant.WithLogger(log),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		h.ServeHTTP(httptest.NewRecorder(), newRequest("example.com", "/"))
		assert.Contains(t, buf.String(), "tenant: resolution failed")
		assert.Contains(t, buf.String(), "level=ERROR")
	})

	t.Run("nil logger is ignored", func(t *testing.T) {
		t.Parallel()
		h := tenant.New(
			tenant.WithSources(func(*http.Request) (string, error) { return "", errors.New("db down") }),
			tenant.WithLogger(nil),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

		w := httptest.NewRecorder()
		assert.NotPanics(t, func() { h.ServeHTTP(w, newRequest("example.com", "/")) })
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("resolution overrides pre-existing context tenant", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.New(tenant.WithSources(tenant.Header("X-Tenant-ID"))).
			Middleware()(captureTenant(&id, &ok))

		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "upstream"))
		r.Header.Set("X-Tenant-ID", "resolved")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		require.True(t, ok)
		assert.Equal(t, "resolved", id)
	})

	t.Run("Context source slots upstream tenant into precedence", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.New(tenant.WithSources(tenant.Context(), tenant.Header("X-Tenant-ID"))).
			Middleware()(captureTenant(&id, &ok))

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
		h := tenant.New(tenant.WithSources(tenant.Header("X-Tenant-ID"))).
			Middleware()(captureTenant(&id, &ok))

		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "upstream"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		require.True(t, ok)
		assert.Equal(t, "upstream", id)
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
