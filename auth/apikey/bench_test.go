package apikey_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/apikey"
)

func BenchmarkCreate(b *testing.B) {
	mgr := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyHit(b *testing.B) {
	// Touch disabled: measures the steady-state verify path.
	mgr := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(-1))
	ctx := context.Background()
	_, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Verify(ctx, plaintext); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyMalformedReject(b *testing.B) {
	// The DoS-relevant path: checksum rejection without store access.
	// Target: zero allocations.
	mgr := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	_, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	if err != nil {
		b.Fatal(err)
	}
	bad := plaintext[:len(plaintext)-1] + "!"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Verify(ctx, bad); err == nil {
			b.Fatal("expected rejection")
		}
	}
}
