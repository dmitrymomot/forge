package tenant_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/tenant"
)

func newRequest(host, path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://placeholder"+path, nil)
	r.Host = host
	return r
}

func TestSubdomain(t *testing.T) {
	t.Parallel()

	resolve := tenant.Subdomain("app.example.com")
	tests := []struct{ name, host, want string }{
		{"single label", "acme.app.example.com", "acme"},
		{"bare base", "app.example.com", ""},
		{"nested labels", "a.b.app.example.com", ""},
		{"unrelated host", "other.example.org", ""},
		{"suffix but not label boundary", "notapp.example.com", ""},
		{"embedded base not suffix", "app.example.com.evil.io", ""},
		{"with port", "acme.app.example.com:8443", "acme"},
		{"uppercase", "ACME.App.Example.COM", "acme"},
		{"trailing FQDN dot", "acme.app.example.com.", "acme"},
		{"empty host", "", ""},
		{"dot only prefix", ".app.example.com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, err := resolve(newRequest(tt.host, "/"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, id)
		})
	}

	t.Run("base is normalized at construction", func(t *testing.T) {
		t.Parallel()
		resolve := tenant.Subdomain("App.Example.COM:443")
		id, err := resolve(newRequest("acme.app.example.com", "/"))
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("empty base panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Subdomain("") })
	})
}

type lookupFunc func(ctx context.Context, domain string) (string, error)

func (f lookupFunc) TenantByDomain(ctx context.Context, domain string) (string, error) {
	return f(ctx, domain)
}

func TestDomain(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		t.Parallel()
		resolve := tenant.Domain(tenant.StaticDomains(map[string]string{"shop.acme.com": "acme"}))
		id, err := resolve(newRequest("shop.acme.com", "/"))
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("not found continues chain", func(t *testing.T) {
		t.Parallel()
		resolve := tenant.Domain(tenant.StaticDomains(nil))
		id, err := resolve(newRequest("unknown.example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("lookup receives normalized host", func(t *testing.T) {
		t.Parallel()
		var got string
		resolve := tenant.Domain(lookupFunc(func(_ context.Context, domain string) (string, error) {
			got = domain
			return "acme", nil
		}))
		_, err := resolve(newRequest("Shop.Acme.COM:8443", "/"))
		require.NoError(t, err)
		assert.Equal(t, "shop.acme.com", got)
	})

	t.Run("infrastructure error stops chain", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("db down")
		resolve := tenant.Domain(lookupFunc(func(context.Context, string) (string, error) {
			return "", boom
		}))
		_, err := resolve(newRequest("shop.acme.com", "/"))
		require.ErrorIs(t, err, boom)
	})

	t.Run("nil lookup panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilLookup, func() { tenant.Domain(nil) })
	})
}

func TestStaticDomains(t *testing.T) {
	t.Parallel()

	lookup := tenant.StaticDomains(map[string]string{
		"Shop.Acme.com:443": "acme",
		"":                  "dropped",
		"empty.example.com": "",
	})

	t.Run("keys normalized at construction", func(t *testing.T) {
		t.Parallel()
		id, err := lookup.TenantByDomain(context.Background(), "shop.acme.com")
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("lookup input normalized", func(t *testing.T) {
		t.Parallel()
		id, err := lookup.TenantByDomain(context.Background(), "SHOP.acme.com.")
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("unknown domain", func(t *testing.T) {
		t.Parallel()
		_, err := lookup.TenantByDomain(context.Background(), "other.example.com")
		require.ErrorIs(t, err, tenant.ErrDomainNotFound)
	})

	t.Run("empty-ID entries dropped", func(t *testing.T) {
		t.Parallel()
		_, err := lookup.TenantByDomain(context.Background(), "empty.example.com")
		require.ErrorIs(t, err, tenant.ErrDomainNotFound)
	})
}

func TestHeader(t *testing.T) {
	t.Parallel()

	resolve := tenant.Header("X-Tenant-ID")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		id, err := resolve(r)
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, err := resolve(newRequest("example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("empty name panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Header("") })
	})
}

func TestCookie(t *testing.T) {
	t.Parallel()

	resolve := tenant.Cookie("tenant")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.AddCookie(&http.Cookie{Name: "tenant", Value: "acme"})
		id, err := resolve(r)
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, err := resolve(newRequest("example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("empty name panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Cookie("") })
	})
}

func TestPathPrefix(t *testing.T) {
	t.Parallel()

	t.Run("with prefix", func(t *testing.T) {
		t.Parallel()
		resolve := tenant.PathPrefix("/t")
		tests := []struct{ name, path, want string }{
			{"segment after prefix", "/t/acme/dashboard", "acme"},
			{"segment only", "/t/acme", "acme"},
			{"trailing slash", "/t/acme/", "acme"},
			{"prefix only", "/t", ""},
			{"prefix with bare slash", "/t/", ""},
			{"prefix not a segment boundary", "/team/acme", ""},
			{"other path", "/orders", ""},
			{"root", "/", ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				id, err := resolve(newRequest("example.com", tt.path))
				require.NoError(t, err)
				assert.Equal(t, tt.want, id)
			})
		}
	})

	t.Run("empty prefix takes first segment", func(t *testing.T) {
		t.Parallel()
		resolve := tenant.PathPrefix("")
		id, err := resolve(newRequest("example.com", "/acme/dashboard"))
		require.NoError(t, err)
		assert.Equal(t, "acme", id)

		id, err = resolve(newRequest("example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("invalid prefix panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrInvalidPrefix, func() { tenant.PathPrefix("t") })
		assert.PanicsWithValue(t, tenant.ErrInvalidPrefix, func() { tenant.PathPrefix("/t/") })
	})
}

func TestContextResolver(t *testing.T) {
	t.Parallel()

	resolve := tenant.Context()

	t.Run("stamped upstream", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "acme"))
		id, err := resolve(r)
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, err := resolve(newRequest("example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})
}
