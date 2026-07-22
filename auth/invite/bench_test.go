package invite_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/invite"
)

func BenchmarkCreate(b *testing.B) {
	mgr := invite.New(invite.NewMemoryStore())
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1", Role: "editor"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPeek(b *testing.B) {
	// The token-verification path minus the consuming write.
	mgr := invite.New(invite.NewMemoryStore())
	ctx := context.Background()
	_, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Peek(ctx, plaintext); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPeekMalformedReject(b *testing.B) {
	// The DoS-relevant path: checksum rejection without store access.
	// Target: zero allocations.
	mgr := invite.New(invite.NewMemoryStore())
	ctx := context.Background()
	_, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
	if err != nil {
		b.Fatal(err)
	}
	bad := plaintext[:len(plaintext)-1] + "!"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Peek(ctx, bad); err == nil {
			b.Fatal("expected rejection")
		}
	}
}

func BenchmarkListPending(b *testing.B) {
	mgr := invite.New(invite.NewMemoryStore())
	ctx := context.Background()
	for range 100 {
		if _, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1"}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		out, err := mgr.List(ctx, invite.Filter{Tenant: "t1", Pending: true})
		if err != nil {
			b.Fatal(err)
		}
		if len(out) != 100 {
			b.Fatalf("got %d", len(out))
		}
	}
}
