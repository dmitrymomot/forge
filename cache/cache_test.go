package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/cache"
)

func TestCacheSetGetTyped(t *testing.T) {
	store := cache.NewMemoryStore()
	defer func() { _ = store.Close() }()
	c := cache.New[string](store, cache.WithPrefix("greet:"))

	require.NoError(t, c.Set(t.Context(), "en", "hello", 0))
	v, err := c.Get(t.Context(), "en")
	require.NoError(t, err)
	assert.Equal(t, "hello", v)
}

func TestCacheMissIsErrNotFound(t *testing.T) {
	store := cache.NewMemoryStore()
	defer func() { _ = store.Close() }()
	c := cache.New[int](store)
	_, err := c.Get(t.Context(), "nope")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestCacheIsolationByPrefixOverSharedStore(t *testing.T) {
	store := cache.NewMemoryStore()
	defer func() { _ = store.Close() }()
	a := cache.New[string](store, cache.WithPrefix("a:"))
	b := cache.New[string](store, cache.WithPrefix("b:"))

	require.NoError(t, a.Set(t.Context(), "k", "AAA", -1))
	require.NoError(t, b.Set(t.Context(), "k", "BBB", -1))

	av, _ := a.Get(t.Context(), "k")
	bv, _ := b.Get(t.Context(), "k")
	assert.Equal(t, "AAA", av)
	assert.Equal(t, "BBB", bv)

	require.NoError(t, a.Clear(t.Context()))
	_, err := a.Get(t.Context(), "k")
	assert.ErrorIs(t, err, cache.ErrNotFound)

	still, err := b.Get(t.Context(), "k") // b untouched by a.Clear
	require.NoError(t, err)
	assert.Equal(t, "BBB", still)
}

func TestGetOrSetStampede(t *testing.T) {
	store := cache.NewMemoryStore()
	defer func() { _ = store.Close() }()
	c := cache.New[int](store)

	var calls atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			<-start
			v, err := c.GetOrSet(t.Context(), "k", func(context.Context) (int, time.Duration, error) {
				calls.Add(1)
				time.Sleep(25 * time.Millisecond)
				return 99, time.Minute, nil
			})
			assert.NoError(t, err)
			assert.Equal(t, 99, v)
		})
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())

	// value is now cached; loader is not called again
	v, err := c.GetOrSet(t.Context(), "k", func(context.Context) (int, time.Duration, error) {
		t.Fatal("loader must not run on hit")
		return 0, 0, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 99, v)
}

func TestGetOrSetSurfacesClosedStore(t *testing.T) {
	store := cache.NewMemoryStore()
	require.NoError(t, store.Close())
	c := cache.New[int](store)
	_, err := c.GetOrSet(t.Context(), "k", func(context.Context) (int, time.Duration, error) {
		t.Fatal("loader must not run when the Store errors")
		return 0, 0, nil
	})
	assert.ErrorIs(t, err, cache.ErrClosed) // existing sentinel passes through
}

// errStore is a Store stub whose Get fails with a non-sentinel error.
type errStore struct{ getErr error }

func (e errStore) Get(context.Context, string) ([]byte, error)            { return nil, e.getErr }
func (errStore) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (errStore) Delete(context.Context, string) error                     { return nil }
func (errStore) Has(context.Context, string) (bool, error)                { return false, nil }
func (errStore) DeletePrefix(context.Context, string) error               { return nil }
func (errStore) Close() error                                             { return nil }

func TestGetOrSetWrapsUnclassifiedStoreError(t *testing.T) {
	boom := errors.New("connection reset")
	c := cache.New[int](errStore{getErr: boom})
	_, err := c.GetOrSet(t.Context(), "k", func(context.Context) (int, time.Duration, error) {
		t.Fatal("loader must not run when the Store errors")
		return 0, 0, nil
	})
	assert.ErrorIs(t, err, cache.ErrStore) // tagged with our sentinel
	assert.ErrorIs(t, err, boom)           // original preserved for unwrapping
}
