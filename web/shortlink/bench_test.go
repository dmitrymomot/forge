package shortlink_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/shortlink"
)

func benchManager(b *testing.B, opts ...shortlink.Option) (*shortlink.Manager, string) {
	b.Helper()
	mgr := shortlink.New(shortlink.NewMemoryStore(), opts...)
	l, err := mgr.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com/dest"})
	if err != nil {
		b.Fatal(err)
	}
	return mgr, l.Code
}

func BenchmarkResolve(b *testing.B) {
	mgr, code := benchManager(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Resolve(ctx, code); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolve_CacheHit(b *testing.B) {
	c := cache.NewMemoryStore()
	defer func() { _ = c.Close() }()
	mgr, code := benchManager(b, shortlink.WithCache(c))
	ctx := context.Background()
	if _, err := mgr.Resolve(ctx, code); err != nil { // warm the cache
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Resolve(ctx, code); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCreate_Generated(b *testing.B) {
	mgr := shortlink.New(shortlink.NewMemoryStore())
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com/dest"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHandler(b *testing.B) {
	mgr, code := benchManager(b)
	mux := http.NewServeMux()
	mux.Handle("GET /{code}", mgr.Handler())
	req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	b.ReportAllocs()
	for b.Loop() {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			b.Fatalf("status %d", w.Code)
		}
	}
}
