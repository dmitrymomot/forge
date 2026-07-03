package ctxkey_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

func BenchmarkKey_With(b *testing.B) {
	k := ctxkey.New[int]("n")
	ctx := context.Background()
	b.ReportAllocs()
	for range b.N {
		_ = k.With(ctx, 1)
	}
}

func BenchmarkKey_From(b *testing.B) {
	k := ctxkey.New[int]("n")
	ctx := k.With(context.Background(), 42)
	b.ReportAllocs()
	for range b.N {
		_, _ = k.From(ctx)
	}
}

func BenchmarkKey_MustFrom(b *testing.B) {
	k := ctxkey.New[int]("n")
	ctx := k.With(context.Background(), 42)
	b.ReportAllocs()
	for range b.N {
		_ = k.MustFrom(ctx)
	}
}
