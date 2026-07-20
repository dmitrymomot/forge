//go:build integration

package pgworkflow_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	pgworkflow "github.com/dmitrymomot/forge/async/workflow/postgres"
)

func BenchmarkStore(b *testing.B) {
	pool := openPool(b)
	store, err := pgworkflow.New(pool)
	require.NoError(b, err)
	ctx := context.Background()

	b.Run("create", func(b *testing.B) {
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			i++
			if err := store.Create(ctx, testRun(fmt.Sprintf("bench-create-%d", i))); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("get", func(b *testing.B) {
		require.NoError(b, store.Create(ctx, testRun("bench-get")))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := store.Get(ctx, "bench-get"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("update", func(b *testing.B) {
		require.NoError(b, store.Create(ctx, testRun("bench-update")))
		run, err := store.Get(ctx, "bench-update")
		require.NoError(b, err)
		b.ReportAllocs()
		for b.Loop() {
			if err := store.Update(ctx, run); err != nil {
				b.Fatal(err)
			}
			run.Version++
		}
	})
}
