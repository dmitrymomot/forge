//go:build integration

package pgeventbus_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	pgeventbus "github.com/dmitrymomot/forge/async/eventbus/postgres"
)

func BenchmarkInboxSeen(b *testing.B) {
	pool := openPool(b)
	ctx := context.Background()
	inbox, err := pgeventbus.NewInbox("bench.consumer")
	require.NoError(b, err)

	b.Run("first_claim", func(b *testing.B) {
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			i++
			id := fmt.Sprintf("bench-first-%d", i)
			if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				_, err := inbox.Seen(ctx, tx, id)
				return err
			}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("duplicate", func(b *testing.B) {
		require.NoError(b, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			_, err := inbox.Seen(ctx, tx, "bench-dup")
			return err
		}))
		b.ReportAllocs()
		for b.Loop() {
			if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
				seen, err := inbox.Seen(ctx, tx, "bench-dup")
				if err == nil && !seen {
					b.Fatal("expected duplicate")
				}
				return err
			}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
