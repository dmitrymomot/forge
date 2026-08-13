package flash_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/view/flash"
	"github.com/dmitrymomot/forge/web/cookie"
)

func benchCodec(b *testing.B) *cookie.Codec {
	b.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		b.Fatal(err)
	}
	c, err := cookie.New(ks)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

func benchStores(b *testing.B) map[string]flash.Store {
	b.Helper()
	cookieStore, err := flash.NewCookieStore(benchCodec(b))
	if err != nil {
		b.Fatal(err)
	}
	mem := cache.NewMemoryStore()
	b.Cleanup(func() { _ = mem.Close() })
	cacheStore, err := flash.NewCacheStore(mem, benchCodec(b))
	if err != nil {
		b.Fatal(err)
	}
	return map[string]flash.Store{"cookie": cookieStore, "cache": cacheStore}
}

func BenchmarkSet(b *testing.B) {
	msgs := []flash.Message{flash.Success("the invoice is sent")}
	for name, s := range benchStores(b) {
		b.Run(name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodPost, "/pay", nil)
			w := httptest.NewRecorder()
			b.ReportAllocs()
			for b.Loop() {
				if err := s.Set(w, req, msgs...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTakeMiss(b *testing.B) {
	for name, s := range benchStores(b) {
		b.Run(name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, "/invoices", nil)
			w := httptest.NewRecorder()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.Take(w, req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTakeHit(b *testing.B) {
	msgs := []flash.Message{flash.Success("the invoice is sent")}
	for name, s := range benchStores(b) {
		b.Run(name, func(b *testing.B) {
			setRec := httptest.NewRecorder()
			if err := s.Set(setRec, httptest.NewRequest(http.MethodPost, "/pay", nil), msgs...); err != nil {
				b.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/invoices", nil)
			for _, c := range setRec.Result().Cookies() {
				req.AddCookie(c)
			}
			w := httptest.NewRecorder()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.Take(w, req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
