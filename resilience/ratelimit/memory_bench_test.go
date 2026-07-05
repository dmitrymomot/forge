package ratelimit_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

// keySet builds n distinct keys with the given prefix; n must be a power of two
// so callers can index with i&(n-1) cheaply inside the hot loop (no per-op
// allocation, so the benchmark measures the store/limiter, not fmt.Sprintf).
func keySet(prefix string, n int) []string {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = prefix + strconv.Itoa(i)
	}
	return keys
}

// BenchmarkMemoryStore_Incr_HotKey is the worst case: every parallel goroutine
// contends on the single global mutex AND the same map entry.
func BenchmarkMemoryStore_Incr_HotKey(b *testing.B) {
	s := ratelimit.NewMemoryStore()
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = s.Incr(ctx, "hot", 1, time.Minute)
		}
	})
}

// BenchmarkMemoryStore_Incr_ManyKeys spreads writes across a bounded keyspace
// (realistic per-IP working set): only the global mutex is contended, not
// individual entries.
func BenchmarkMemoryStore_Incr_ManyKeys(b *testing.B) {
	const n = 4096
	keys := keySet("k", n)
	s := ratelimit.NewMemoryStore()
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = s.Incr(ctx, keys[i&(n-1)], 1, time.Minute)
			i++
		}
	})
}

// BenchmarkMemoryStore_GetMostlyMiss exercises the read path on absent keys.
func BenchmarkMemoryStore_GetMostlyMiss(b *testing.B) {
	const n = 4096
	keys := keySet("miss", n)
	s := ratelimit.NewMemoryStore()
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = s.Get(ctx, keys[i&(n-1)])
			i++
		}
	})
}

// BenchmarkLimiter_Allow is the end-to-end request path (1 Incr + 1 Get + the
// sliding-window math) on the default single-lock store. Its ops/sec maps
// directly to sustainable requests/sec.
func BenchmarkLimiter_Allow(b *testing.B) {
	const n = 4096
	keys := keySet("user", n)
	l := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.WithLimit(1_000_000, time.Minute))
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = l.Allow(ctx, keys[i&(n-1)])
			i++
		}
	})
}

// BenchmarkLimiter_Allow_Sharded is the same end-to-end path over a 16-shard
// store; compare its ns/op against BenchmarkLimiter_Allow to see the sharding
// lift the single-mutex contention ceiling.
func BenchmarkLimiter_Allow_Sharded(b *testing.B) {
	const n = 4096
	keys := keySet("user", n)
	l := ratelimit.New(ratelimit.NewMemoryStore(ratelimit.WithShards(16)), ratelimit.WithLimit(1_000_000, time.Minute))
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = l.Allow(ctx, keys[i&(n-1)])
			i++
		}
	})
}

// BenchmarkLimiter_Allow_WithJanitorAndLargeMap is a pathological stress case: a
// 200k-entry map of long-lived entries (so nothing is ever evicted) swept by an
// aggressive 1ms janitor, so every sweep is a full O(200k) scan holding the
// single global lock while Allow runs concurrently. Compare against
// BenchmarkLimiter_Allow to isolate the sweep's impact, and against the sharded
// variant below to see the per-shard sweep flatten the cliff.
func BenchmarkLimiter_Allow_WithJanitorAndLargeMap(b *testing.B) {
	const n = 4096
	keys := keySet("live", n)
	store := ratelimit.NewMemoryStore(ratelimit.WithMemoryJanitor(time.Millisecond))
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	for i := range 200_000 { // 1h TTL: every sweep scans all of them and deletes none
		_, _ = store.Incr(ctx, "stale"+strconv.Itoa(i), 1, time.Hour)
	}
	l := ratelimit.New(store, ratelimit.WithLimit(1_000_000, time.Minute))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = l.Allow(ctx, keys[i&(n-1)])
			i++
		}
	})
}

// BenchmarkLimiter_Allow_WithJanitorAndLargeMap_Sharded is the same pathological
// case over 16 shards: each sweep locks one shard (~1/16 of the map) at a time,
// so concurrent Allow no longer stalls behind a full-map scan.
func BenchmarkLimiter_Allow_WithJanitorAndLargeMap_Sharded(b *testing.B) {
	const n = 4096
	keys := keySet("live", n)
	store := ratelimit.NewMemoryStore(ratelimit.WithShards(16), ratelimit.WithMemoryJanitor(time.Millisecond))
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	for i := range 200_000 {
		_, _ = store.Incr(ctx, "stale"+strconv.Itoa(i), 1, time.Hour)
	}
	l := ratelimit.New(store, ratelimit.WithLimit(1_000_000, time.Minute))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = l.Allow(ctx, keys[i&(n-1)])
			i++
		}
	})
}
