package jwt_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

func benchKeysetHS(b *testing.B) *keyset.Keyset {
	b.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		b.Fatalf("keyset: %v", err)
	}
	return ks
}

func benchKeysetEd(b *testing.B) *keyset.Keyset {
	b.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("ed25519: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		b.Fatalf("pkcs8: %v", err)
	}
	ks, err := keyset.New(keyset.WithPrimary(1, der))
	if err != nil {
		b.Fatalf("keyset: %v", err)
	}
	return ks
}

func benchClaims() jwt.Claims {
	return jwt.Claims{
		Issuer:    "https://api.example.com",
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
}

func BenchmarkSignHS256(b *testing.B) {
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(benchKeysetHS(b)))
	if err != nil {
		b.Fatalf("NewSigner: %v", err)
	}
	c := benchClaims()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Sign(c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyHS256(b *testing.B) {
	ks := benchKeysetHS(b)
	s, err := jwt.NewSigner(jwt.WithHS256Keyset(ks))
	if err != nil {
		b.Fatalf("NewSigner: %v", err)
	}
	v, err := jwt.NewVerifier(jwt.WithVerifyHS256Keyset(ks))
	if err != nil {
		b.Fatalf("NewVerifier: %v", err)
	}
	tok, err := s.Sign(benchClaims())
	if err != nil {
		b.Fatalf("Sign: %v", err)
	}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := jwt.Verify[jwt.Claims](ctx, v, tok); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSignEdDSA(b *testing.B) {
	s, err := jwt.NewSigner(jwt.WithKeyset(benchKeysetEd(b)))
	if err != nil {
		b.Fatalf("NewSigner: %v", err)
	}
	c := benchClaims()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Sign(c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyEdDSA(b *testing.B) {
	ks := benchKeysetEd(b)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	if err != nil {
		b.Fatalf("NewSigner: %v", err)
	}
	v, err := jwt.NewVerifier(jwt.WithVerifyKeyset(ks))
	if err != nil {
		b.Fatalf("NewVerifier: %v", err)
	}
	tok, err := s.Sign(benchClaims())
	if err != nil {
		b.Fatalf("Sign: %v", err)
	}
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := jwt.Verify[jwt.Claims](ctx, v, tok); err != nil {
			b.Fatal(err)
		}
	}
}
