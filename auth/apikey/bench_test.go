package apikey_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/apikey"
)

// benchKey mints one key outside the measured loop. The effects stay
// trivial closures so the numbers measure this package, not a fixture's
// map and mutex.
func benchKey(b *testing.B) (apikey.Config, apikey.Key, string) {
	b.Helper()
	cfg := mustConfig(b, apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(-1))
	k, plaintext := issueKey(b, cfg, apikey.CreateParams{Subject: "u1"})
	return cfg, k, plaintext
}

func BenchmarkCreate(b *testing.B) {
	cfg := mustConfig(b, apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, discardKey); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyHit(b *testing.B) {
	cfg, k, plaintext := benchKey(b)
	load := loadsKeyByHash(k)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := apikey.Verify(ctx, cfg, plaintext, load, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerifyHitCurried measures the same path through the curried
// guard.Verifier, so the factory's closure cost is visible against
// BenchmarkVerifyHit.
func BenchmarkVerifyHitCurried(b *testing.B) {
	cfg, k, plaintext := benchKey(b)
	verifier, err := apikey.NewVerifier(cfg, loadsKeyByHash(k), nil)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := verifier.Verify(ctx, plaintext); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerifyMalformedReject measures the DoS-relevant path: checksum
// rejection with no storage access. Target: zero allocations.
func BenchmarkVerifyMalformedReject(b *testing.B) {
	cfg, k, plaintext := benchKey(b)
	load := loadsKeyByHash(k)
	bad := plaintext[:len(plaintext)-1] + "!"
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := apikey.Verify(ctx, cfg, bad, load, nil); err == nil {
			b.Fatal("expected rejection")
		}
	}
}
