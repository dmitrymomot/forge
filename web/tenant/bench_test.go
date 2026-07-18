package tenant_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/tenant"
)

func BenchmarkSubdomain(b *testing.B) {
	derive := tenant.Subdomain("app.example.com", tenant.StaticSubdomains(map[string]string{"acme": "t_01acme"}))
	r := newRequest("acme.app.example.com", "/")
	b.ReportAllocs()
	for b.Loop() {
		if id, _ := derive(r); id == "" {
			b.Fatal("expected resolution")
		}
	}
}

func BenchmarkSubdomainMiss(b *testing.B) {
	derive := tenant.Subdomain("app.example.com", tenant.StaticSubdomains(map[string]string{"acme": "t_01acme"}))
	r := newRequest("other.example.org", "/")
	b.ReportAllocs()
	for b.Loop() {
		if id, _ := derive(r); id != "" {
			b.Fatal("unexpected resolution")
		}
	}
}

func BenchmarkHeader(b *testing.B) {
	derive := tenant.Header("X-Tenant-ID")
	r := newRequest("example.com", "/")
	r.Header.Set("X-Tenant-ID", "acme")
	b.ReportAllocs()
	for b.Loop() {
		if id, _ := derive(r); id == "" {
			b.Fatal("expected resolution")
		}
	}
}

func BenchmarkPathPrefix(b *testing.B) {
	derive := tenant.PathPrefix("/t")
	r := newRequest("example.com", "/t/acme/dashboard")
	b.ReportAllocs()
	for b.Loop() {
		if id, _ := derive(r); id == "" {
			b.Fatal("expected resolution")
		}
	}
}

func BenchmarkDomainStatic(b *testing.B) {
	derive := tenant.Domain(tenant.StaticDomains(map[string]string{"shop.acme.com": "acme"}))
	r := newRequest("shop.acme.com", "/")
	b.ReportAllocs()
	for b.Loop() {
		if id, _ := derive(r); id == "" {
			b.Fatal("expected resolution")
		}
	}
}

func BenchmarkResolve(b *testing.B) {
	rv := tenant.New(
		tenant.WithSources(
			tenant.Domain(tenant.StaticDomains(map[string]string{"shop.acme.com": "t_01acme"})),
			tenant.Subdomain("app.example.com", tenant.StaticSubdomains(map[string]string{"acme": "t_01acme"})),
		),
		tenant.WithValidator(tenant.ValidatorFunc(func(context.Context, string) error { return nil })),
	)
	r := newRequest("acme.app.example.com", "/")
	b.ReportAllocs()
	for b.Loop() {
		if id, _ := rv.Resolve(r); id == "" {
			b.Fatal("expected resolution")
		}
	}
}

func BenchmarkMiddleware(b *testing.B) {
	h := tenant.New(tenant.WithSources(
		tenant.Domain(tenant.StaticDomains(map[string]string{"shop.acme.com": "t_01acme"})),
		tenant.Subdomain("app.example.com", tenant.StaticSubdomains(map[string]string{"acme": "t_01acme"})),
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
