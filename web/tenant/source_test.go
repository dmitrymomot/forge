package tenant_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/tenant"
)

func newRequest(host, path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://placeholder"+path, nil)
	r.Host = host
	return r
}

type subdomainLookupFunc func(ctx context.Context, subdomain string) (string, error)

func (f subdomainLookupFunc) TenantBySubdomain(ctx context.Context, subdomain string) (string, error) {
	return f(ctx, subdomain)
}

func TestSubdomain(t *testing.T) {
	t.Parallel()

	lookup := tenant.StaticSubdomains(map[string]string{"acme": "t_01acme"})
	derive := tenant.Subdomain("app.example.com", lookup)
	tests := []struct{ name, host, want string }{
		{"single label", "acme.app.example.com", "t_01acme"},
		{"unknown label continues chain", "other.app.example.com", ""},
		{"bare base", "app.example.com", ""},
		{"nested labels", "a.b.app.example.com", ""},
		{"unrelated host", "other.example.org", ""},
		{"suffix but not label boundary", "notapp.example.com", ""},
		{"embedded base not suffix", "app.example.com.evil.io", ""},
		{"with port", "acme.app.example.com:8443", "t_01acme"},
		{"uppercase", "ACME.App.Example.COM", "t_01acme"},
		{"trailing FQDN dot", "acme.app.example.com.", "t_01acme"},
		{"empty host", "", ""},
		{"dot only prefix", ".app.example.com", ""},
		{"ipv6 literal", "[::1]:8080", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, err := derive(newRequest(tt.host, "/"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, id)
		})
	}

	t.Run("base is normalized at construction", func(t *testing.T) {
		t.Parallel()
		derive := tenant.Subdomain("App.Example.COM:443", lookup)
		id, err := derive(newRequest("acme.app.example.com", "/"))
		require.NoError(t, err)
		assert.Equal(t, "t_01acme", id)
	})

	t.Run("lookup receives the normalized label", func(t *testing.T) {
		t.Parallel()
		var got string
		derive := tenant.Subdomain("app.example.com", subdomainLookupFunc(func(_ context.Context, subdomain string) (string, error) {
			got = subdomain
			return "t_1", nil
		}))
		_, err := derive(newRequest("ACME.app.example.com:8443", "/"))
		require.NoError(t, err)
		assert.Equal(t, "acme", got)
	})

	t.Run("lookup skipped when no label matches", func(t *testing.T) {
		t.Parallel()
		derive := tenant.Subdomain("app.example.com", subdomainLookupFunc(func(context.Context, string) (string, error) {
			t.Fatal("lookup must not run without a matched label")
			return "", nil
		}))
		id, err := derive(newRequest("app.example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("infrastructure error stops chain", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("db down")
		derive := tenant.Subdomain("app.example.com", subdomainLookupFunc(func(context.Context, string) (string, error) {
			return "", boom
		}))
		_, err := derive(newRequest("acme.app.example.com", "/"))
		require.ErrorIs(t, err, boom)
	})

	t.Run("empty base panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Subdomain("", lookup) })
	})

	t.Run("nil lookup panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilLookup, func() { tenant.Subdomain("app.example.com", nil) })
	})
}

func TestStaticSubdomains(t *testing.T) {
	t.Parallel()

	lookup := tenant.StaticSubdomains(map[string]string{
		"Acme":  "t_01acme",
		"":      "dropped",
		"empty": "",
	})

	t.Run("keys lowercased at construction", func(t *testing.T) {
		t.Parallel()
		id, err := lookup.TenantBySubdomain(context.Background(), "acme")
		require.NoError(t, err)
		assert.Equal(t, "t_01acme", id)
	})

	t.Run("unknown label", func(t *testing.T) {
		t.Parallel()
		_, err := lookup.TenantBySubdomain(context.Background(), "other")
		require.ErrorIs(t, err, tenant.ErrTenantNotFound)
	})

	t.Run("empty-key and empty-ID entries dropped", func(t *testing.T) {
		t.Parallel()
		_, err := lookup.TenantBySubdomain(context.Background(), "")
		require.ErrorIs(t, err, tenant.ErrTenantNotFound)
		_, err = lookup.TenantBySubdomain(context.Background(), "empty")
		require.ErrorIs(t, err, tenant.ErrTenantNotFound)
	})
}

func TestMap(t *testing.T) {
	t.Parallel()

	slugToID := func(_ context.Context, slug string) (string, error) {
		if slug == "acme" {
			return "t_01acme", nil
		}
		return "", tenant.ErrTenantNotFound
	}

	t.Run("translates derived value", func(t *testing.T) {
		t.Parallel()
		derive := tenant.Map(tenant.PathPrefix("/t"), slugToID)
		id, err := derive(newRequest("example.com", "/t/acme/dashboard"))
		require.NoError(t, err)
		assert.Equal(t, "t_01acme", id)
	})

	t.Run("ErrTenantNotFound continues chain", func(t *testing.T) {
		t.Parallel()
		derive := tenant.Map(tenant.PathPrefix("/t"), slugToID)
		id, err := derive(newRequest("example.com", "/t/other/dashboard"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("fn skipped when inner source misses", func(t *testing.T) {
		t.Parallel()
		derive := tenant.Map(tenant.PathPrefix("/t"), func(context.Context, string) (string, error) {
			t.Fatal("fn must not run for an underived value")
			return "", nil
		})
		id, err := derive(newRequest("example.com", "/orders"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("inner source error propagates without fn", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("inner failed")
		derive := tenant.Map(func(*http.Request) (string, error) { return "", boom }, slugToID)
		_, err := derive(newRequest("example.com", "/"))
		require.ErrorIs(t, err, boom)
	})

	t.Run("fn error stops chain", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("db down")
		derive := tenant.Map(tenant.PathPrefix("/t"), func(context.Context, string) (string, error) {
			return "", boom
		})
		_, err := derive(newRequest("example.com", "/t/acme"))
		require.ErrorIs(t, err, boom)
	})

	t.Run("nil arguments panic", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrNilSource, func() { tenant.Map(nil, slugToID) })
		assert.PanicsWithValue(t, tenant.ErrNilLookup, func() { tenant.Map(tenant.Context(), nil) })
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
		derive := tenant.Domain(tenant.StaticDomains(map[string]string{"shop.acme.com": "acme"}))
		id, err := derive(newRequest("shop.acme.com", "/"))
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("not found continues chain", func(t *testing.T) {
		t.Parallel()
		derive := tenant.Domain(tenant.StaticDomains(nil))
		id, err := derive(newRequest("unknown.example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("empty and malformed hosts skip the lookup", func(t *testing.T) {
		t.Parallel()
		derive := tenant.Domain(lookupFunc(func(context.Context, string) (string, error) {
			t.Fatal("lookup must not run without a normalized host")
			return "", nil
		}))
		for _, host := range []string{"", "[::1"} {
			id, err := derive(newRequest(host, "/"))
			require.NoError(t, err)
			assert.Empty(t, id)
		}
	})

	t.Run("lookup receives normalized host", func(t *testing.T) {
		t.Parallel()
		var got string
		derive := tenant.Domain(lookupFunc(func(_ context.Context, domain string) (string, error) {
			got = domain
			return "acme", nil
		}))
		_, err := derive(newRequest("Shop.Acme.COM:8443", "/"))
		require.NoError(t, err)
		assert.Equal(t, "shop.acme.com", got)
	})

	t.Run("infrastructure error stops chain", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("db down")
		derive := tenant.Domain(lookupFunc(func(context.Context, string) (string, error) {
			return "", boom
		}))
		_, err := derive(newRequest("shop.acme.com", "/"))
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
		require.ErrorIs(t, err, tenant.ErrTenantNotFound)
	})

	t.Run("empty-ID entries dropped", func(t *testing.T) {
		t.Parallel()
		_, err := lookup.TenantByDomain(context.Background(), "empty.example.com")
		require.ErrorIs(t, err, tenant.ErrTenantNotFound)
	})
}

func TestHeader(t *testing.T) {
	t.Parallel()

	derive := tenant.Header("X-Tenant-ID")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "acme")
		id, err := derive(r)
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, err := derive(newRequest("example.com", "/"))
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

	derive := tenant.Cookie("tenant")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.AddCookie(&http.Cookie{Name: "tenant", Value: "acme"})
		id, err := derive(r)
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, err := derive(newRequest("example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("empty value reads as not resolved", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.Header.Set("Cookie", "tenant=")
		id, err := derive(r)
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("empty name panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Cookie("") })
	})
}

func TestQuery(t *testing.T) {
	t.Parallel()

	derive := tenant.Query("tenant")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		id, err := derive(newRequest("example.com", "/orders?tenant=acme"))
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, err := derive(newRequest("example.com", "/orders"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("empty value reads as not resolved", func(t *testing.T) {
		t.Parallel()
		id, err := derive(newRequest("example.com", "/orders?tenant="))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("empty name panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Query("") })
	})
}

func TestPathPrefix(t *testing.T) {
	t.Parallel()

	t.Run("with prefix", func(t *testing.T) {
		t.Parallel()
		derive := tenant.PathPrefix("/t")
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
				id, err := derive(newRequest("example.com", tt.path))
				require.NoError(t, err)
				assert.Equal(t, tt.want, id)
			})
		}
	})

	t.Run("empty prefix takes first segment", func(t *testing.T) {
		t.Parallel()
		derive := tenant.PathPrefix("")
		id, err := derive(newRequest("example.com", "/acme/dashboard"))
		require.NoError(t, err)
		assert.Equal(t, "acme", id)

		id, err = derive(newRequest("example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("invalid prefix panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrInvalidPrefix, func() { tenant.PathPrefix("t") })
		assert.PanicsWithValue(t, tenant.ErrInvalidPrefix, func() { tenant.PathPrefix("/t/") })
	})
}

func TestContextSource(t *testing.T) {
	t.Parallel()

	derive := tenant.Context()

	t.Run("stamped upstream", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "acme"))
		id, err := derive(r)
		require.NoError(t, err)
		assert.Equal(t, "acme", id)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		id, err := derive(newRequest("example.com", "/"))
		require.NoError(t, err)
		assert.Empty(t, id)
	})
}
