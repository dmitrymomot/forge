package shortlink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/shortlink"
)

func TestResolve_Basics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com/dest"})
	require.NoError(t, err)

	got, err := mgr.Resolve(ctx, l.Code)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/dest", got.URL)

	_, err = mgr.Resolve(ctx, "nope")
	assert.ErrorIs(t, err, shortlink.ErrNotFound)

	_, err = mgr.Resolve(ctx, "")
	assert.ErrorIs(t, err, shortlink.ErrNotFound)
}

func TestResolve_Expired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore())

	l, err := mgr.Create(ctx, shortlink.CreateParams{
		URL: "https://example.com", ExpiresAt: time.Now().UTC().Add(-time.Second),
	})
	require.NoError(t, err)

	_, err = mgr.Resolve(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrLinkExpired)

	future, err := mgr.Create(ctx, shortlink.CreateParams{
		URL: "https://example.com", ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)
	_, err = mgr.Resolve(ctx, future.Code)
	assert.NoError(t, err)
}

func TestResolve_OnHit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var hits []shortlink.Link
	store := shortlink.NewMemoryStore()
	mgr := shortlink.New(store, shortlink.WithOnHit(func(_ context.Context, l shortlink.Link) {
		hits = append(hits, l)
	}))

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	_, err = mgr.Resolve(ctx, l.Code)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, l.Code, hits[0].Code)
	assert.Equal(t, l.URL, hits[0].URL)

	_, _ = mgr.Resolve(ctx, "missing")
	assert.Len(t, hits, 1, "failed resolves must not fire OnHit")

	require.NoError(t, mgr.Deactivate(ctx, l.Code))
	_, _ = mgr.Resolve(ctx, l.Code)
	assert.Len(t, hits, 1, "deactivated resolves must not fire OnHit")
}

// countingStore counts Get calls to observe cache read-through.
type countingStore struct {
	shortlink.Store
	gets int
}

func (s *countingStore) Get(ctx context.Context, code string) (shortlink.Link, error) {
	s.gets++
	return s.Store.Get(ctx, code)
}

func newCache(t *testing.T) cache.Store {
	t.Helper()
	c := cache.NewMemoryStore()
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestResolve_CacheReadThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &countingStore{Store: shortlink.NewMemoryStore()}
	mgr := shortlink.New(store, shortlink.WithCache(newCache(t)))

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	for range 3 {
		got, err := mgr.Resolve(ctx, l.Code)
		require.NoError(t, err)
		assert.Equal(t, l.URL, got.URL)
	}
	assert.Equal(t, 1, store.gets, "second and third resolves must be cache hits")
}

func TestResolve_CacheInvalidatedOnMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithCache(newCache(t)))

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	_, err = mgr.Resolve(ctx, l.Code) // warm the cache
	require.NoError(t, err)

	require.NoError(t, mgr.Deactivate(ctx, l.Code))
	_, err = mgr.Resolve(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrLinkDeactivated, "deactivation must not be masked by the cache")

	require.NoError(t, mgr.Activate(ctx, l.Code))
	_, err = mgr.Resolve(ctx, l.Code)
	assert.NoError(t, err)

	_, err = mgr.Resolve(ctx, l.Code) // re-warm after activation
	require.NoError(t, err)
	require.NoError(t, mgr.Delete(ctx, l.Code))
	_, err = mgr.Resolve(ctx, l.Code)
	assert.ErrorIs(t, err, shortlink.ErrNotFound, "deletion must not be masked by the cache")
}

// failingCache errors on every operation to prove Resolve degrades to the
// store and mutations surface invalidation failures.
type failingCache struct{ cache.Store }

var errCacheDown = errors.New("cache down")

func (failingCache) Get(context.Context, string) ([]byte, error) { return nil, errCacheDown }
func (failingCache) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return errCacheDown
}
func (failingCache) Delete(context.Context, string) error { return errCacheDown }

func TestResolve_CacheFailureFallsBackToStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithCache(failingCache{}))

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	got, err := mgr.Resolve(ctx, l.Code)
	require.NoError(t, err)
	assert.Equal(t, l.URL, got.URL)
}

func TestMutation_SurfacesCacheInvalidateFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mgr := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithCache(failingCache{}))

	l, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	err = mgr.Deactivate(ctx, l.Code)
	require.ErrorIs(t, err, errCacheDown)

	// The store mutation itself landed even though invalidation failed.
	got, err := mgr.Get(ctx, l.Code)
	require.NoError(t, err)
	assert.False(t, got.DeactivatedAt.IsZero())
}
