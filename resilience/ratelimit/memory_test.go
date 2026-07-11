package ratelimit_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func TestMemoryStore_IncrGetResetExpiry(t *testing.T) {
	mk := clock.NewMock(time.Unix(1000, 0))
	s := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk))
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	n, err := s.Incr(ctx, "k", 1, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = s.Incr(ctx, "k", 2, time.Minute) // does not extend TTL
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	got, err := s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(3), got)

	mk.Advance(61 * time.Second) // past TTL
	got, err = s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got) // expired

	got, _ = s.Get(ctx, "absent")
	assert.Equal(t, int64(0), got)
}

func TestMemoryStore_ConcurrentIncr(t *testing.T) {
	s := ratelimit.NewMemoryStore()
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	const g = 100
	done := make(chan struct{}, g)
	for range g {
		go func() { _, _ = s.Incr(ctx, "c", 1, time.Minute); done <- struct{}{} }()
	}
	for range g {
		<-done
	}
	n, _ := s.Get(ctx, "c")
	assert.Equal(t, int64(g), n)
}

func TestMemoryStore_JanitorEvictsExpiredEntries(t *testing.T) {
	mk := clock.NewMock(time.Unix(1000, 0))
	s := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk), ratelimit.WithMemoryJanitor(5*time.Millisecond))
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	lener, ok := s.(interface{ Len() int })
	require.True(t, ok, "memoryStore must expose Len() int")

	_, err := s.Incr(ctx, "a", 1, time.Minute)
	require.NoError(t, err)
	_, err = s.Incr(ctx, "b", 1, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 2, lener.Len())

	mk.Advance(2 * time.Minute) // both entries now expired by the clock

	assert.Eventually(t, func() bool {
		return lener.Len() == 0
	}, time.Second, 5*time.Millisecond, "janitor should evict expired entries")
}

func TestMemoryStore_CloseStopsJanitorCleanlyAndIsIdempotent(t *testing.T) {
	mk := clock.NewMock(time.Unix(1000, 0))
	s := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk), ratelimit.WithMemoryJanitor(5*time.Millisecond))
	ctx := context.Background()

	_, err := s.Incr(ctx, "a", 1, time.Minute)
	require.NoError(t, err)

	require.NoError(t, s.Close())
	require.NoError(t, s.Close()) // idempotent, must not hang or panic

	lener, ok := s.(interface{ Len() int })
	require.True(t, ok)
	before := lener.Len()

	mk.Advance(2 * time.Minute)
	time.Sleep(20 * time.Millisecond) // give a leaked goroutine a chance to misbehave

	assert.Equal(t, before, lener.Len(), "janitor must not run after Close")
}

// TestMemoryStore_ShardedIncrGetReset confirms that with sharding a key routes
// consistently to one shard, so counts accumulate and Reset clears correctly.
func TestMemoryStore_ShardedIncrGetReset(t *testing.T) {
	s := ratelimit.NewMemoryStore(ratelimit.WithShards(8))
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	for range 5 {
		_, err := s.Incr(ctx, "k", 1, time.Minute)
		require.NoError(t, err)
	}
	got, err := s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(5), got)

	require.NoError(t, s.Reset(ctx, "k"))
	got, err = s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got)
}

// TestMemoryStore_ShardedConcurrentIncr hammers several keys across shards from
// many goroutines and asserts no updates are lost (race-clean under -race).
func TestMemoryStore_ShardedConcurrentIncr(t *testing.T) {
	s := ratelimit.NewMemoryStore(ratelimit.WithShards(16))
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	keys := []string{"a", "b", "c", "d"}
	const goroutines = 200
	const perG = 50
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range perG {
				_, _ = s.Incr(ctx, keys[(g+i)%len(keys)], 1, time.Minute)
			}
		})
	}
	wg.Wait()

	var total int64
	for _, k := range keys {
		v, _ := s.Get(ctx, k)
		total += v
	}
	assert.Equal(t, int64(goroutines*perG), total, "no updates lost across shards")
}

// TestMemoryStore_ShardedJanitorEvictsAcrossShards confirms the per-shard sweep
// evicts expired entries from every shard, not just one.
func TestMemoryStore_ShardedJanitorEvictsAcrossShards(t *testing.T) {
	mk := clock.NewMock(time.Unix(1000, 0))
	s := ratelimit.NewMemoryStore(
		ratelimit.WithMemoryClock(mk),
		ratelimit.WithShards(8),
		ratelimit.WithMemoryJanitor(5*time.Millisecond),
	)
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	lener, ok := s.(interface{ Len() int })
	require.True(t, ok)

	for i := range 500 {
		_, err := s.Incr(ctx, "key"+strconv.Itoa(i), 1, time.Minute)
		require.NoError(t, err)
	}
	assert.Equal(t, 500, lener.Len())

	mk.Advance(2 * time.Minute)
	assert.Eventually(t, func() bool {
		return lener.Len() == 0
	}, time.Second, 5*time.Millisecond, "janitor should evict expired entries across all shards")
}

// TestMemoryStore_WithShardsInvalidDefaultsToOne confirms an invalid shard count
// is ignored (store still works as the single-lock default).
func TestMemoryStore_WithShardsInvalidDefaultsToOne(t *testing.T) {
	ctx := context.Background()
	for _, n := range []int{0, -1} {
		s := ratelimit.NewMemoryStore(ratelimit.WithShards(n))
		got, err := s.Incr(ctx, "k", 3, time.Minute)
		require.NoError(t, err)
		assert.Equal(t, int64(3), got)
		require.NoError(t, s.Close())
	}
}

func TestMemoryStore_NonPositiveTTLNeverExpires(t *testing.T) {
	clk := clock.NewMock(time.Unix(0, 0))
	s := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(clk))
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()

	n, err := s.Incr(ctx, "gauge", 5, 0) // ttl == 0 → no expiry
	require.NoError(t, err)
	assert.Equal(t, int64(5), n)

	clk.Advance(1000 * time.Hour) // far past any TTL
	got, err := s.Get(ctx, "gauge")
	require.NoError(t, err)
	assert.Equal(t, int64(5), got) // still present

	n, err = s.Incr(ctx, "gauge", -2, -1) // negative ttl also = no expiry
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}
