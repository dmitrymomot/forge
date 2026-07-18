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

// idLookup resolves KindID identifiers against a fixed set of live tenants;
// everything else is not found. The default consumer shape for tests.
func idLookup(live ...string) tenant.Lookup {
	return tenant.LookupFunc(func(_ context.Context, ident tenant.Identifier) (string, error) {
		if ident.Kind != tenant.KindID {
			return "", tenant.ErrTenantNotFound
		}
		for _, id := range live {
			if ident.Value == id {
				return id, nil
			}
		}
		return "", tenant.ErrTenantNotFound
	})
}

// captureTenant records the tenant seen by the wrapped handler.
func captureTenant(id *string, ok *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*id, *ok = tenant.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("nil lookup panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilLookup, func() {
			tenant.New(nil, tenant.WithSources(tenant.Context()))
		})
	})

	t.Run("no sources panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNoSources, func() { tenant.New(idLookup()) })
	})

	t.Run("nil source panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilSource, func() {
			tenant.WithSources(tenant.Context(), nil)
		})
	})

	t.Run("nil error handler panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilComponent, func() { tenant.WithErrorHandler(nil) })
	})
}

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("first extracted identifier wins", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(idLookup("alpha", "beta"), tenant.WithSources(
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

	t.Run("lookup receives the tagged identifier", func(t *testing.T) {
		t.Parallel()
		var got tenant.Identifier
		rv := tenant.New(
			tenant.LookupFunc(func(_ context.Context, ident tenant.Identifier) (string, error) {
				got = ident
				return "t_01acme", nil
			}),
			tenant.WithSources(tenant.Subdomain("app.example.com")),
		)
		id, err := rv.Resolve(newRequest("ACME.app.example.com:8443", "/"))
		require.NoError(t, err)
		assert.Equal(t, "t_01acme", id)
		assert.Equal(t, tenant.Identifier{Kind: tenant.KindSubdomain, Value: "acme"}, got)
	})

	t.Run("not found continues chain to a later source", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.LookupFunc(func(_ context.Context, ident tenant.Identifier) (string, error) {
				if ident.Kind == tenant.KindSubdomain && ident.Value == "acme" {
					return "t_01acme", nil
				}
				return "", tenant.ErrTenantNotFound
			}),
			tenant.WithSources(
				tenant.Domain(), // extracts on every host; lookup says not found
				tenant.Subdomain("app.example.com"),
			),
		)
		id, err := rv.Resolve(newRequest("acme.app.example.com", "/"))
		require.NoError(t, err)
		assert.Equal(t, "t_01acme", id)
	})

	t.Run("repeated WithSources appends in order", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(idLookup("alpha", "beta"),
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

	t.Run("nothing extracts returns ErrNoTenant without calling lookup", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				t.Fatal("lookup must not run without an extracted identifier")
				return "", nil
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
		)
		_, err := rv.Resolve(newRequest("example.com", "/"))
		require.ErrorIs(t, err, tenant.ErrNoTenant)
	})

	t.Run("nothing resolves returns ErrNoTenant", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(idLookup(), tenant.WithSources(tenant.Header("X-Tenant-ID")))
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "ghost")
		_, err := rv.Resolve(r)
		require.ErrorIs(t, err, tenant.ErrNoTenant)
	})

	t.Run("inactive fails closed without trying later sources", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.LookupFunc(func(_ context.Context, ident tenant.Identifier) (string, error) {
				if ident.Value == "stale" {
					return "", tenant.ErrTenantInactive
				}
				return ident.Value, nil
			}),
			tenant.WithSources(tenant.Header("X-Stale"), tenant.Header("X-Live")),
		)
		r := newRequest("example.com", "/")
		r.Header.Set("X-Stale", "stale")
		r.Header.Set("X-Live", "live")
		_, err := rv.Resolve(r)
		require.ErrorIs(t, err, tenant.ErrTenantInactive)
	})

	t.Run("infrastructure error fails closed", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("db down")
		rv := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				return "", boom
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID"), tenant.Header("X-Backup")),
		)
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		r.Header.Set("X-Backup", "other")
		_, err := rv.Resolve(r)
		require.ErrorIs(t, err, boom)
	})

	t.Run("empty id with nil error fails closed as a lookup bug", func(t *testing.T) {
		t.Parallel()
		rv := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				return "", nil
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
		)
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		_, err := rv.Resolve(r)
		require.Error(t, err)
		assert.NotErrorIs(t, err, tenant.ErrNoTenant)
		assert.NotErrorIs(t, err, tenant.ErrTenantNotFound)
	})
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("stamps resolved tenant", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.New(idLookup("acme"), tenant.WithSources(tenant.Header("X-Tenant-ID"))).
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
		h := tenant.New(idLookup(), tenant.WithSources(tenant.Header("X-Tenant-ID"))).
			Middleware()(captureTenant(&id, &ok))

		w := httptest.NewRecorder()
		h.ServeHTTP(w, newRequest("example.com", "/"))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, ok)
	})

	t.Run("not found on every source passes through untenanted", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.New(idLookup(), tenant.WithSources(tenant.Domain(), tenant.Header("X-Tenant-ID"))).
			Middleware()(captureTenant(&id, &ok))

		r := newRequest("marketing.example.com", "/")
		r.Header.Set("X-Tenant-ID", "ghost")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, ok)
	})

	t.Run("infrastructure error responds 500 and skips next", func(t *testing.T) {
		t.Parallel()
		called := false
		h := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				return "", errors.New("db down")
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.False(t, called)
	})

	t.Run("inactive tenant responds 404 and skips next", func(t *testing.T) {
		t.Parallel()
		called := false
		h := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				return "", tenant.ErrTenantInactive
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.False(t, called)
	})

	t.Run("ErrNoTenant from the lookup fails closed, never passes through", func(t *testing.T) {
		t.Parallel()
		called := false
		h := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				return "", tenant.ErrNoTenant
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.False(t, called)
	})

	t.Run("custom error handler receives the resolution error", func(t *testing.T) {
		t.Parallel()
		var got error
		h := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				return "", tenant.ErrTenantInactive
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error) {
				got = err
				w.WriteHeader(http.StatusTeapot)
			}),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		assert.Equal(t, http.StatusTeapot, w.Code)
		require.ErrorIs(t, got, tenant.ErrTenantInactive)
	})

	t.Run("rejections log at debug, infrastructure errors at error", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		inactive := tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
			return "", tenant.ErrTenantInactive
		})
		h := tenant.New(inactive,
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithLogger(log),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		h.ServeHTTP(httptest.NewRecorder(), r)
		assert.Contains(t, buf.String(), "tenant: resolution failed")
		assert.Contains(t, buf.String(), "level=DEBUG")

		buf.Reset()
		broken := tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
			return "", errors.New("db down")
		})
		h = tenant.New(broken,
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithLogger(log),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		h.ServeHTTP(httptest.NewRecorder(), r)
		assert.Contains(t, buf.String(), "tenant: resolution failed")
		assert.Contains(t, buf.String(), "level=ERROR")
	})

	t.Run("nil logger is ignored", func(t *testing.T) {
		t.Parallel()
		h := tenant.New(
			tenant.LookupFunc(func(context.Context, tenant.Identifier) (string, error) {
				return "", errors.New("db down")
			}),
			tenant.WithSources(tenant.Header("X-Tenant-ID")),
			tenant.WithLogger(nil),
		).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		w := httptest.NewRecorder()
		assert.NotPanics(t, func() { h.ServeHTTP(w, r) })
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("resolution overrides pre-existing context tenant", func(t *testing.T) {
		t.Parallel()
		var id string
		var ok bool
		h := tenant.New(idLookup("resolved"), tenant.WithSources(tenant.Header("X-Tenant-ID"))).
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
		h := tenant.New(idLookup("upstream", "header"),
			tenant.WithSources(tenant.Context(), tenant.Header("X-Tenant-ID"))).
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
		h := tenant.New(idLookup(), tenant.WithSources(tenant.Header("X-Tenant-ID"))).
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
