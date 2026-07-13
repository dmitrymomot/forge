package guard_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

// nopWriter discards the response so benchmarks measure guard's work, not
// httptest.ResponseRecorder's buffer growth.
type nopWriter struct{ h http.Header }

func (w *nopWriter) Header() http.Header         { return w.h }
func (w *nopWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *nopWriter) WriteHeader(int)             {}

func benchVerifier() guard.Verifier {
	id := guard.Identity{Subject: "u1", Tenant: "t1", Method: "bearer"}
	return guard.VerifierFunc(func(context.Context, string) (guard.Identity, error) {
		return id, nil
	})
}

func BenchmarkNew_ValidBearer(b *testing.B) {
	var depth int
	h := guard.New(benchVerifier())(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if id, ok := guard.From(r.Context()); ok {
			depth += len(id.Subject)
		}
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer tok")
	w := &nopWriter{h: make(http.Header)}
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, r)
	}
	_ = depth
}

func BenchmarkNew_Reject401(b *testing.B) {
	h := guard.New(benchVerifier(), guard.WithChallenge(`Bearer realm="api"`))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest(http.MethodGet, "/", nil) // no credential
	w := &nopWriter{h: make(http.Header)}
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, r)
	}
}

func BenchmarkExtractors(b *testing.B) {
	bearer := httptest.NewRequest(http.MethodGet, "/", nil)
	bearer.Header.Set("Authorization", "Bearer tok123")
	apikey := httptest.NewRequest(http.MethodGet, "/", nil)
	apikey.Header.Set("X-API-Key", "key123")
	withCookie := httptest.NewRequest(http.MethodGet, "/", nil)
	withCookie.AddCookie(&http.Cookie{Name: "sid", Value: "abc"})
	withQuery := httptest.NewRequest(http.MethodGet, "/p?a=1&token=qtok&c=3", nil)

	benches := []struct {
		name string
		x    guard.Extractor
		r    *http.Request
	}{
		{"BearerHeader", guard.BearerHeader(), bearer},
		{"Header", guard.Header("X-API-Key"), apikey},
		{"Cookie", guard.Cookie("sid"), withCookie},
		{"Query", guard.Query("token"), withQuery},
	}
	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := bb.x(bb.r); !ok {
					b.Fatal("extractor missed")
				}
			}
		})
	}
}

func BenchmarkBasicAuth(b *testing.B) {
	users := map[string]string{"ops": "s3cret"}
	inner := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	good := httptest.NewRequest(http.MethodGet, "/", nil)
	good.SetBasicAuth("ops", "s3cret")
	bad := httptest.NewRequest(http.MethodGet, "/", nil)
	bad.SetBasicAuth("ops", "wrong")

	h := guard.BasicAuth(users)(inner)
	w := &nopWriter{h: make(http.Header)}

	b.Run("success", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			h.ServeHTTP(w, good)
		}
	})
	b.Run("wrong-password", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			h.ServeHTTP(w, bad)
		}
	})
}

func BenchmarkLogExtractor(b *testing.B) {
	var ctx context.Context
	h := guard.New(benchVerifier())(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(&nopWriter{h: make(http.Header)}, r)

	b.ReportAllocs()
	for b.Loop() {
		if _, ok := guard.LogExtractor(ctx); !ok {
			b.Fatal("no identity in ctx")
		}
	}
}
