package cache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/cache"
	"github.com/dmitrymomot/forge/clock"
)

func TestMemoryStoreSetGetDelete(t *testing.T) {
	s := cache.NewMemoryStore()
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), 0))
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
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), 30*time.Second))
	clk.Advance(31 * time.Second)
	_, err := s.Get(t.Context(), "k")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestMemoryStoreNegativeTTLNeverExpires(t *testing.T) {
	clk := clock.NewMock(time.Now())
	s := cache.NewMemoryStore(cache.WithClock(clk))
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), -1))
	clk.Advance(1000 * time.Hour)
	got, err := s.Get(t.Context(), "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("v"), got)
}

func TestMemoryStoreLRUEviction(t *testing.T) {
	s := cache.NewMemoryStore(cache.WithMaxEntries(2))
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "a", []byte("1"), -1))
	require.NoError(t, s.Set(t.Context(), "b", []byte("2"), -1))
	_, _ = s.Get(t.Context(), "a") // touch a -> b is least-recently-used
	require.NoError(t, s.Set(t.Context(), "c", []byte("3"), -1))

	_, err := s.Get(t.Context(), "b")
	assert.ErrorIs(t, err, cache.ErrNotFound)
	_, err = s.Get(t.Context(), "a")
	assert.NoError(t, err)
}

func TestMemoryStoreDeletePrefix(t *testing.T) {
	s := cache.NewMemoryStore()
	defer s.Close()

	require.NoError(t, s.Set(t.Context(), "a:1", []byte("x"), -1))
	require.NoError(t, s.Set(t.Context(), "a:2", []byte("x"), -1))
	require.NoError(t, s.Set(t.Context(), "b:1", []byte("x"), -1))

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
	err := s.Set(t.Context(), "k", []byte("v"), 0)
	assert.ErrorIs(t, err, cache.ErrClosed)
}

// Smoke test: exercises the janitor goroutine start/sweep/stop path under -race.
// The janitor uses a real ticker, so this asserts no panic/leak, not timing.
func TestMemoryStoreJanitorStartsAndStops(t *testing.T) {
	s := cache.NewMemoryStore(cache.WithCleanupInterval(5 * time.Millisecond))
	require.NoError(t, s.Set(t.Context(), "k", []byte("v"), time.Millisecond))
	time.Sleep(20 * time.Millisecond) // let the ticker fire at least once
	require.NoError(t, s.Close())     // must stop the goroutine cleanly
}
