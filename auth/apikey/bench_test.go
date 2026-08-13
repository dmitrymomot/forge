package apikey_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/apikey"
)

// benchKey mints one key outside the measured loop. The effects stay
// trivial closures so the numbers measure this package, not a fixture's
// map and mutex.
func benchKey(b *testing.B) (*apikey.Manager, apikey.Key, string) {
	b.Helper()
	mgr := mustManager(b, apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(-1))
	k, plaintext := issueKey(b, mgr, apikey.CreateParams{Subject: "u1"})
	return mgr, k, plaintext
}

func BenchmarkCreate(b *testing.B) {
	mgr := mustManager(b, apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"}, discardKey); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyHit(b *testing.B) {
	mgr, k, plaintext := benchKey(b)
	load := loadsKeyByHash(k)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Verify(ctx, plaintext, load, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerifyHitCurried measures the same path through the curried
// guard.Verifier, so the factory's closure cost is visible against
// BenchmarkVerifyHit.
func BenchmarkVerifyHitCurried(b *testing.B) {
	mgr, k, plaintext := benchKey(b)
	verifier, err := mgr.Verifier(loadsKeyByHash(k), nil)
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
	mgr, k, plaintext := benchKey(b)
	load := loadsKeyByHash(k)
	bad := plaintext[:len(plaintext)-1] + "!"
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Verify(ctx, bad, load, nil); err == nil {
			b.Fatal("expected rejection")
		}
	}
}
