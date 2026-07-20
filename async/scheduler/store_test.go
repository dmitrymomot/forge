package scheduler_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/scheduler"
)

func TestMemoryStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tick := at("2026-07-20T10:00:00Z")

	t.Run("claim once", func(t *testing.T) {
		t.Parallel()
		st := scheduler.NewMemoryStore()
		require.NoError(t, st.Claim(ctx, "job", tick))
		require.ErrorIs(t, st.Claim(ctx, "job", tick), scheduler.ErrAlreadyClaimed)
		// Other names and ticks are independent.
		require.NoError(t, st.Claim(ctx, "other", tick))
		require.NoError(t, st.Claim(ctx, "job", tick.Add(time.Minute)))
	})

	t.Run("empty name rejected", func(t *testing.T) {
		t.Parallel()
		st := scheduler.NewMemoryStore()
		require.Error(t, st.Claim(ctx, "", tick))
	})

	t.Run("keyed by instant not location", func(t *testing.T) {
		t.Parallel()
		st := scheduler.NewMemoryStore()
		require.NoError(t, st.Claim(ctx, "job", tick))
		require.ErrorIs(t, st.Claim(ctx, "job", tick.In(time.FixedZone("plus2", 7200))), scheduler.ErrAlreadyClaimed)
	})

	t.Run("release reopens the tick", func(t *testing.T) {
		t.Parallel()
		st := scheduler.NewMemoryStore()
		require.NoError(t, st.Claim(ctx, "job", tick))
		require.NoError(t, st.Release(ctx, "job", tick))
		require.NoError(t, st.Claim(ctx, "job", tick))
		// Releasing an absent claim is a no-op.
		require.NoError(t, st.Release(ctx, "ghost", tick))
	})

	t.Run("purge before cutoff", func(t *testing.T) {
		t.Parallel()
		st := scheduler.NewMemoryStore()
		require.NoError(t, st.Claim(ctx, "job", tick))
		require.NoError(t, st.Claim(ctx, "job", tick.Add(time.Hour)))
		require.NoError(t, st.Claim(ctx, "other", tick))
		n, err := st.PurgeBefore(ctx, tick.Add(time.Minute))
		require.NoError(t, err)
		assert.Equal(t, 2, n)
		// Purged ticks are claimable again; the kept one is not.
		require.NoError(t, st.Claim(ctx, "job", tick))
		require.ErrorIs(t, st.Claim(ctx, "job", tick.Add(time.Hour)), scheduler.ErrAlreadyClaimed)
	})

	t.Run("concurrent claims elect one winner", func(t *testing.T) {
		t.Parallel()
		st := scheduler.NewMemoryStore()
		const racers = 32
		var wg sync.WaitGroup
		wins := make(chan struct{}, racers)
		for range racers {
			wg.Go(func() {
				if st.Claim(ctx, "job", tick) == nil {
					wins <- struct{}{}
				}
			})
		}
		wg.Wait()
		assert.Len(t, wins, 1)
	})
}
