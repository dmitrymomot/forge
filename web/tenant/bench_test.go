package tenant_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/tenant"
)

// benchLookup resolves subdomain "acme" and domain "shop.acme.com" to fixed
// IDs and echoes KindID values, mimicking a consumer's single-query lookup.
var benchLookup = tenant.LookupFunc(func(_ context.Context, ident tenant.Identifier) (string, error) {
	switch ident.Kind {
	case tenant.KindSubdomain:
		if ident.Value == "acme" {
			return "t_01acme", nil
		}
	case tenant.KindDomain:
		if ident.Value == "shop.acme.com" {
			return "t_01acme", nil
		}
	case tenant.KindID:
		return ident.Value, nil
	}
	return "", tenant.ErrTenantNotFound
})

func BenchmarkSubdomain(b *testing.B) {
	extract := tenant.Subdomain("app.example.com")
	r := newRequest("acme.app.example.com", "/")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := extract(r); !ok {
			b.Fatal("expected extraction")
		}
	}
}

func BenchmarkSubdomainMiss(b *testing.B) {
	extract := tenant.Subdomain("app.example.com")
	r := newRequest("other.example.org", "/")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := extract(r); ok {
			b.Fatal("unexpected extraction")
		}
	}
}

func BenchmarkDomain(b *testing.B) {
	extract := tenant.Domain()
	r := newRequest("shop.acme.com", "/")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := extract(r); !ok {
			b.Fatal("expected extraction")
		}
	}
}

func BenchmarkHeader(b *testing.B) {
	extract := tenant.Header("X-Tenant-ID")
	r := newRequest("example.com", "/")
	r.Header.Set("X-Tenant-ID", "t_01acme")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := extract(r); !ok {
			b.Fatal("expected extraction")
		}
	}
}

func BenchmarkQuery(b *testing.B) {
	extract := tenant.Query("tenant")
	r := newRequest("example.com", "/orders?utm_source=x&tenant=t_01acme")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := extract(r); !ok {
			b.Fatal("expected extraction")
		}
	}
}

func BenchmarkPathPrefix(b *testing.B) {
	extract := tenant.PathPrefix("/t")
	r := newRequest("example.com", "/t/acme/dashboard")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := extract(r); !ok {
			b.Fatal("expected extraction")
		}
	}
}

func BenchmarkResolve(b *testing.B) {
	rv := tenant.New(benchLookup, tenant.WithSources(
		tenant.Domain(),
		tenant.Subdomain("app.example.com"),
	))
	r := newRequest("acme.app.example.com", "/")
	b.ReportAllocs()
	for b.Loop() {
		if id, _ := rv.Resolve(r); id == "" {
			b.Fatal("expected resolution")
		}
	}
}

func BenchmarkMiddleware(b *testing.B) {
	h := tenant.New(benchLookup, tenant.WithSources(
		tenant.Domain(),
		tenant.Subdomain("app.example.com"),
	)).Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := newRequest("acme.app.example.com", "/")
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, r)
	}
}

func BenchmarkFromContext(b *testing.B) {
	ctx := tenant.NewContext(context.Background(), "acme")
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := tenant.FromContext(ctx); !ok {
			b.Fatal("expected tenant")
		}
	}
}
