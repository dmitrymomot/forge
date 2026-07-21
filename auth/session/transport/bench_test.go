package transport_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/session/transport"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

func BenchmarkCookie_Extract(b *testing.B) {
	tr := transport.Cookie()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "tok-123456789"})
	b.ReportAllocs()
	for b.Loop() {
		if tr.Extract(r) == "" {
			b.Fatal("no token")
		}
	}
}

func BenchmarkBearer_Extract(b *testing.B) {
	tr := transport.Bearer()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer tok-123456789")
	b.ReportAllocs()
	for b.Loop() {
		if tr.Extract(r) == "" {
			b.Fatal("no token")
		}
	}
}

func BenchmarkJWT_Extract(b *testing.B) {
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		b.Fatal(err)
	}
	signer, err := jwt.NewSigner(jwt.WithHS256Keyset(ks))
	if err != nil {
		b.Fatal(err)
	}
	verifier, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(ks))
	if err != nil {
		b.Fatal(err)
	}
	tr := transport.JWT(signer, verifier)
	w := httptest.NewRecorder()
	if err := tr.Embed(w, "opaque-tok", time.Now().Add(time.Hour)); err != nil {
		b.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+w.Header().Get("X-Session-Token"))
	b.ReportAllocs()
	for b.Loop() {
		if tr.Extract(r) == "" {
			b.Fatal("no token")
		}
	}
}
