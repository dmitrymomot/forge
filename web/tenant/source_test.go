package tenant_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/tenant"
)

func newRequest(host, path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://placeholder"+path, nil)
	r.Host = host
	return r
}

func TestSubdomain(t *testing.T) {
	t.Parallel()

	extract := tenant.Subdomain("app.example.com")
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
		{"ipv6 literal", "[::1]:8080", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ident, ok := extract(newRequest(tt.host, "/"))
			if tt.want == "" {
				assert.False(t, ok)
				assert.Zero(t, ident)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, tenant.Identifier{Kind: tenant.KindSubdomain, Value: tt.want}, ident)
		})
	}

	t.Run("base is normalized at construction", func(t *testing.T) {
		t.Parallel()
		extract := tenant.Subdomain("App.Example.COM:443")
		ident, ok := extract(newRequest("acme.app.example.com", "/"))
		assert.True(t, ok)
		assert.Equal(t, "acme", ident.Value)
	})

	t.Run("empty base panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Subdomain("") })
	})
}

func TestDomain(t *testing.T) {
	t.Parallel()

	extract := tenant.Domain()

	t.Run("extracts normalized host", func(t *testing.T) {
		t.Parallel()
		ident, ok := extract(newRequest("Shop.Acme.COM:8443", "/"))
		assert.True(t, ok)
		assert.Equal(t, tenant.Identifier{Kind: tenant.KindDomain, Value: "shop.acme.com"}, ident)
	})

	t.Run("empty and malformed hosts do not extract", func(t *testing.T) {
		t.Parallel()
		for _, host := range []string{"", "[::1"} {
			ident, ok := extract(newRequest(host, "/"))
			assert.False(t, ok)
			assert.Zero(t, ident)
		}
	})
}

func TestHeader(t *testing.T) {
	t.Parallel()

	extract := tenant.Header("X-Tenant-ID")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.Header.Set("X-Tenant-ID", "t_123")
		ident, ok := extract(r)
		assert.True(t, ok)
		assert.Equal(t, tenant.Identifier{Kind: tenant.KindID, Value: "t_123"}, ident)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		_, ok := extract(newRequest("example.com", "/"))
		assert.False(t, ok)
	})

	t.Run("empty name panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Header("") })
	})
}

func TestCookie(t *testing.T) {
	t.Parallel()

	extract := tenant.Cookie("tenant")

	t.Run("present", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.AddCookie(&http.Cookie{Name: "tenant", Value: "t_123"})
		ident, ok := extract(r)
		assert.True(t, ok)
		assert.Equal(t, tenant.Identifier{Kind: tenant.KindID, Value: "t_123"}, ident)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		_, ok := extract(newRequest("example.com", "/"))
		assert.False(t, ok)
	})

	t.Run("empty value reads as not present", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r.Header.Set("Cookie", "tenant=")
		_, ok := extract(r)
		assert.False(t, ok)
	})

	t.Run("empty name panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Cookie("") })
	})
}

func TestQuery(t *testing.T) {
	t.Parallel()

	extract := tenant.Query("tenant")

	tests := []struct{ name, query, want string }{
		{"present", "tenant=t_123", "t_123"},
		{"absent", "other=x", ""},
		{"empty value", "tenant=", ""},
		{"empty query", "", ""},
		{"later pair", "a=1&tenant=t_123", "t_123"},
		{"percent-decoded value", "tenant=t%5F123", "t_123"},
		{"percent-decoded key", "%74enant=t_123", "t_123"},
		{"plus decodes to space", "tenant=+t_123", " t_123"},
		{"bad escape in value skipped", "tenant=%zz", ""},
		{"bad escape in key skips pair only", "%zz=1&tenant=t_123", "t_123"},
		{"semicolon pair skipped", "tenant=evil;tenant=alt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ident, ok := extract(newRequest("example.com", "/orders?"+tt.query))
			if tt.want == "" {
				assert.False(t, ok)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, tenant.Identifier{Kind: tenant.KindID, Value: tt.want}, ident)
		})
	}

	t.Run("empty name panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrEmptyName, func() { tenant.Query("") })
	})
}

func TestPathPrefix(t *testing.T) {
	t.Parallel()

	t.Run("with prefix", func(t *testing.T) {
		t.Parallel()
		extract := tenant.PathPrefix("/t")
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
				ident, ok := extract(newRequest("example.com", tt.path))
				if tt.want == "" {
					assert.False(t, ok)
					return
				}
				assert.True(t, ok)
				assert.Equal(t, tenant.Identifier{Kind: tenant.KindPath, Value: tt.want}, ident)
			})
		}
	})

	t.Run("empty prefix takes first segment", func(t *testing.T) {
		t.Parallel()
		extract := tenant.PathPrefix("")
		ident, ok := extract(newRequest("example.com", "/acme/dashboard"))
		assert.True(t, ok)
		assert.Equal(t, "acme", ident.Value)

		_, ok = extract(newRequest("example.com", "/"))
		assert.False(t, ok)
	})

	t.Run("invalid prefix panics", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, tenant.ErrInvalidPrefix, func() { tenant.PathPrefix("t") })
		assert.PanicsWithValue(t, tenant.ErrInvalidPrefix, func() { tenant.PathPrefix("/t/") })
	})
}

func TestContextSource(t *testing.T) {
	t.Parallel()

	extract := tenant.Context()

	t.Run("stamped upstream", func(t *testing.T) {
		t.Parallel()
		r := newRequest("example.com", "/")
		r = r.WithContext(tenant.NewContext(r.Context(), "t_123"))
		ident, ok := extract(r)
		assert.True(t, ok)
		assert.Equal(t, tenant.Identifier{Kind: tenant.KindID, Value: "t_123"}, ident)
	})

	t.Run("absent", func(t *testing.T) {
		t.Parallel()
		_, ok := extract(newRequest("example.com", "/"))
		assert.False(t, ok)
	})
}
