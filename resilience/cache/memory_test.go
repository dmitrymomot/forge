package cache_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
)

func TestMemoryStoreSetGetDelete(t *testing.T) {
	s := cache.NewMemoryStore()
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v")))
	got, err := s.Get(t.Context(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), got)

	require.NoError(t, s.Delete(t.Context(), "k"))
	_, err = s.Get(t.Context(), "k")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestMemoryStoreExpiry(t *testing.T) {
	clk := clock.NewMock(time.Now())
	s := cache.NewMemoryStore(cache.WithClock(clk))
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), cache.WithTTL(30*time.Second)))
	clk.Advance(31 * time.Second)
	_, err := s.Get(t.Context(), "k")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestMemoryStoreNegativeTTLNeverExpires(t *testing.T) {
	clk := clock.NewMock(time.Now())
	s := cache.NewMemoryStore(cache.WithClock(clk))
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), cache.WithTTL(-1)))
	clk.Advance(1000 * time.Hour)
	got, err := s.Get(t.Context(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), got)
}

func TestMemoryStoreLRUEviction(t *testing.T) {
	s := cache.NewMemoryStore(cache.WithMaxEntries(2))
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "a", []byte("1"), cache.WithTTL(-1)))
	require.NoError(t, s.Set(t.Context(), "b", []byte("2"), cache.WithTTL(-1)))
	_, _ = s.Get(t.Context(), "a") // touch a -> b is least-recently-used
	require.NoError(t, s.Set(t.Context(), "c", []byte("3"), cache.WithTTL(-1)))

	_, err := s.Get(t.Context(), "b")
	assert.ErrorIs(t, err, cache.ErrNotFound)
	_, err = s.Get(t.Context(), "a")
	assert.NoError(t, err)
}

func TestMemoryStoreDeletePrefix(t *testing.T) {
	s := cache.NewMemoryStore()
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "a:1", []byte("x"), cache.WithTTL(-1)))
	require.NoError(t, s.Set(t.Context(), "a:2", []byte("x"), cache.WithTTL(-1)))
	require.NoError(t, s.Set(t.Context(), "b:1", []byte("x"), cache.WithTTL(-1)))

	require.NoError(t, s.DeletePrefix(t.Context(), "a:"))
	_, err := s.Get(t.Context(), "a:1")
	assert.ErrorIs(t, err, cache.ErrNotFound)
	ok, err := s.Has(t.Context(), "b:1")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestMemoryStoreClosedRejects(t *testing.T) {
	s := cache.NewMemoryStore()
	require.NoError(t, s.Close())
	require.NoError(t, s.Close()) // idempotent
	err := s.Set(t.Context(), "k", []byte("v"))
	assert.ErrorIs(t, err, cache.ErrClosed)
}

// Smoke test: exercises the janitor goroutine start/sweep/stop path under -race.
// The janitor uses a real ticker, so this asserts no panic/leak, not timing.
func TestMemoryStoreJanitorStartsAndStops(t *testing.T) {
	s := cache.NewMemoryStore(cache.WithCleanupInterval(5 * time.Millisecond))
	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), cache.WithTTL(time.Millisecond)))
	time.Sleep(20 * time.Millisecond) // let the ticker fire at least once
	require.NoError(t, s.Close())     // must stop the goroutine cleanly
}

func TestMemoryStoreSetNonExistClaimsOnce(t *testing.T) {
	s := cache.NewMemoryStore()
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "k", []byte("first"), cache.WithSetNonExist()))
	assert.ErrorIs(t, s.Set(t.Context(), "k", []byte("second"), cache.WithSetNonExist()), cache.ErrExists)

	got, _ := s.Get(t.Context(), "k")
	assert.Equal(t, []byte("first"), got) // original not overwritten
}

func TestMemoryStoreSetNonExistReclaimsExpired(t *testing.T) {
	clk := clock.NewMock(time.Now())
	s := cache.NewMemoryStore(cache.WithClock(clk))
	defer func() { _ = s.Close() }()

	require.NoError(t, s.Set(t.Context(), "k", []byte("old"), cache.WithSetNonExist(), cache.WithTTL(time.Second)))
	clk.Advance(2 * time.Second) // entry now expired
	require.NoError(t, s.Set(t.Context(), "k", []byte("new"), cache.WithSetNonExist()))
	got, _ := s.Get(t.Context(), "k")
	assert.Equal(t, []byte("new"), got)
}

func TestMemoryStoreSetNonExistConcurrent(t *testing.T) {
	s := cache.NewMemoryStore()
	defer func() { _ = s.Close() }()

	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if err := s.Set(t.Context(), "k", []byte("v"), cache.WithSetNonExist()); err == nil {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins.Load()) // exactly one claim wins
}
