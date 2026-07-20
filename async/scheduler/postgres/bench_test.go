//go:build integration

package pgscheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/scheduler"
	pgscheduler "github.com/dmitrymomot/forge/async/scheduler/postgres"
)

func BenchmarkClaim(b *testing.B) {
	pool := openPool(b)
	store, err := pgscheduler.NewStore(pool)
	require.NoError(b, err)
	ctx := context.Background()
	base := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	b.Run("first_claim", func(b *testing.B) {
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			i++
			if err := store.Claim(ctx, "bench.first", base.Add(time.Duration(i)*time.Second)); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("duplicate", func(b *testing.B) {
		require.NoError(b, store.Claim(ctx, "bench.dup", base))
		b.ReportAllocs()
		for b.Loop() {
			if err := store.Claim(ctx, "bench.dup", base); !errors.Is(err, scheduler.ErrAlreadyClaimed) {
				b.Fatal(err)
			}
		}
	})
}
