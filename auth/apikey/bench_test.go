package apikey_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/apikey"
)

func BenchmarkCreate(b *testing.B) {
	cfg, err := apikey.NewConfig(apikey.WithPrefix("sk_live"))
	if err != nil {
		b.Fatal(err)
	}
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerifyHit measures the steady-state verify path with touching
// disabled.
func BenchmarkVerifyHit(b *testing.B) {
	cfg, err := apikey.NewConfig(apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(-1))
	if err != nil {
		b.Fatal(err)
	}
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	_, plaintext, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerifyHitCurried measures the same path through the curried
// guard.Verifier, so the factory's closure cost is visible against
// BenchmarkVerifyHit.
func BenchmarkVerifyHitCurried(b *testing.B) {
	cfg, err := apikey.NewConfig(apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(-1))
	if err != nil {
		b.Fatal(err)
	}
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	_, plaintext, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	if err != nil {
		b.Fatal(err)
	}
	verifier, err := apikey.NewVerifier(cfg, mem.LoadByHash, mem.Touch)
	if err != nil {
		b.Fatal(err)
	}
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
	cfg, err := apikey.NewConfig(apikey.WithPrefix("sk_live"))
	if err != nil {
		b.Fatal(err)
	}
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	_, plaintext, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	if err != nil {
		b.Fatal(err)
	}
	bad := plaintext[:len(plaintext)-1] + "!"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := apikey.Verify(ctx, cfg, bad, mem.LoadByHash, mem.Touch); err == nil {
			b.Fatal("expected rejection")
		}
	}
}
