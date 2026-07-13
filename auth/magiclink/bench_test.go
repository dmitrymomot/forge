package magiclink_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/magiclink"
	"github.com/dmitrymomot/forge/resilience/cache"
)

func BenchmarkIssue(b *testing.B) {
	m, err := magiclink.New[loginClaims](testKey, "bench")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Issue(ctx, loginClaims{UserID: "u_1"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPeek(b *testing.B) {
	m, err := magiclink.New[loginClaims](testKey, "bench")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	link, err := m.Issue(ctx, loginClaims{UserID: "u_1"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Peek(ctx, link); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRedeemSingleUse(b *testing.B) {
	store := cache.NewMemoryStore()
	defer func() { _ = store.Close() }()
	m, err := magiclink.New[loginClaims](testKey, "bench", magiclink.WithStore(store))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		link, err := m.Issue(ctx, loginClaims{UserID: "u_1"})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := m.Redeem(ctx, link); err != nil {
			b.Fatal(err)
		}
	}
}
