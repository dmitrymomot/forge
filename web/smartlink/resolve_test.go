package smartlink_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/smartlink"
)

// TestResolveLiveness asserts Resolve enforces liveness after retrieval: an
// active Link resolves cleanly, an expired one surfaces ErrLinkExpired, a
// deactivated one surfaces ErrLinkDeactivated, and an unknown code surfaces
// ErrNotFound from the Store.
func TestResolveLiveness(t *testing.T) {
	t.Parallel()
	m := newTestManager(t)
	ctx := context.Background()

	active, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "active1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if got, err := m.Resolve(ctx, active.Code); err != nil {
		t.Fatalf("Resolve(active) error = %v, want nil", err)
	} else if got.Code != active.Code {
		t.Fatalf("Resolve(active).Code = %q, want %q", got.Code, active.Code)
	}

	expired, err := m.Create(ctx, smartlink.CreateParams{
		Target:    "https://example.com/",
		Code:      "expired1",
		ExpiresAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if _, err := m.Resolve(ctx, expired.Code); !errors.Is(err, smartlink.ErrLinkExpired) {
		t.Fatalf("Resolve(expired) = %v, want ErrLinkExpired", err)
	}

	deactivated, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "deact1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if err := m.Deactivate(ctx, deactivated.Code); err != nil {
		t.Fatalf("Deactivate() error = %v, want nil", err)
	}
	if _, err := m.Resolve(ctx, deactivated.Code); !errors.Is(err, smartlink.ErrLinkDeactivated) {
		t.Fatalf("Resolve(deactivated) = %v, want ErrLinkDeactivated", err)
	}

	if _, err := m.Resolve(ctx, "unknown-code"); !errors.Is(err, smartlink.ErrNotFound) {
		t.Fatalf("Resolve(unknown) = %v, want ErrNotFound", err)
	}
}

// countingStore wraps a Store and counts Get calls, to prove cache
// read-through skips the backing Store on a hit.
type countingStore struct {
	smartlink.Store
	gets atomic.Int32
}

func (s *countingStore) Get(ctx context.Context, code string) (smartlink.Link, error) {
	s.gets.Add(1)
	return s.Store.Get(ctx, code)
}

// TestResolveCacheReadThrough asserts a second Resolve for the same code is
// served entirely from the cache: the backing Store's Get is called only
// once, and the cached data matches the store-backed first read.
func TestResolveCacheReadThrough(t *testing.T) {
	t.Parallel()
	store := &countingStore{Store: smartlink.NewMemoryStore()}
	m, err := smartlink.NewManager(store, smartlink.WithCache(cache.NewMemoryStore(), time.Minute))
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	ctx := context.Background()

	created, err := m.Create(ctx, smartlink.CreateParams{
		Target:   "https://example.com/",
		Code:     "cache1",
		Metadata: map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	first, err := m.Resolve(ctx, created.Code)
	if err != nil {
		t.Fatalf("Resolve() #1 error = %v, want nil", err)
	}
	if got := store.gets.Load(); got != 1 {
		t.Fatalf("store Gets after first Resolve = %d, want 1", got)
	}

	second, err := m.Resolve(ctx, created.Code)
	if err != nil {
		t.Fatalf("Resolve() #2 error = %v, want nil", err)
	}
	if got := store.gets.Load(); got != 1 {
		t.Fatalf("store Gets after second Resolve = %d, want 1 (must be served from cache)", got)
	}
	if second.Code != first.Code || second.Target != first.Target || second.Metadata["k"] != "v" {
		t.Fatalf("second Resolve = %+v, want same data as first %+v", second, first)
	}
}

var errCacheBoom = errors.New("cache boundary: unavailable")

// failingCache is a cache.Store whose every method errors, to prove cache
// errors never fail Resolve — the Store stays the source of truth.
type failingCache struct{}

func (failingCache) Get(context.Context, string) ([]byte, error) { return nil, errCacheBoom }
func (failingCache) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return errCacheBoom
}
func (failingCache) Delete(context.Context, string) error       { return errCacheBoom }
func (failingCache) Has(context.Context, string) (bool, error)  { return false, errCacheBoom }
func (failingCache) DeletePrefix(context.Context, string) error { return errCacheBoom }
func (failingCache) Close() error                               { return nil }

// TestResolveCacheErrorFallsThrough asserts a failing cache (Get and Set
// both erroring) never fails Resolve: the Store still serves the Link.
func TestResolveCacheErrorFallsThrough(t *testing.T) {
	t.Parallel()
	m, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithCache(failingCache{}, time.Minute))
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	ctx := context.Background()

	created, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "cerr1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	got, err := m.Resolve(ctx, created.Code)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil (store must still serve on cache error)", err)
	}
	if got.Code != created.Code {
		t.Fatalf("Resolve().Code = %q, want %q", got.Code, created.Code)
	}
}

// TestLifecycleInvalidatesCache asserts Deactivate, Activate, and Delete
// each best-effort evict the cache key after a successful Store mutation,
// bounding staleness of a warmed cache entry to at most the configured TTL.
func TestLifecycleInvalidatesCache(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*smartlink.Manager, cache.Store, string, string) {
		t.Helper()
		cs := cache.NewMemoryStore()
		m, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithCache(cs, time.Minute))
		if err != nil {
			t.Fatalf("NewManager() error = %v, want nil", err)
		}
		ctx := context.Background()
		l, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/"})
		if err != nil {
			t.Fatalf("Create() error = %v, want nil", err)
		}
		if _, err := m.Resolve(ctx, l.Code); err != nil {
			t.Fatalf("Resolve() (warm cache) error = %v, want nil", err)
		}
		key := "smartlink:code:" + l.Code
		if _, err := cs.Get(ctx, key); err != nil {
			t.Fatalf("cache Get() after warm-up = %v, want nil (cached)", err)
		}
		return m, cs, l.Code, key
	}

	t.Run("Deactivate", func(t *testing.T) {
		t.Parallel()
		m, cs, code, key := setup(t)
		ctx := context.Background()
		if err := m.Deactivate(ctx, code); err != nil {
			t.Fatalf("Deactivate() error = %v, want nil", err)
		}
		if _, err := cs.Get(ctx, key); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("cache Get() after Deactivate = %v, want ErrNotFound", err)
		}
	})

	t.Run("Activate", func(t *testing.T) {
		t.Parallel()
		m, cs, code, key := setup(t)
		ctx := context.Background()
		if err := m.Activate(ctx, code); err != nil {
			t.Fatalf("Activate() error = %v, want nil", err)
		}
		if _, err := cs.Get(ctx, key); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("cache Get() after Activate = %v, want ErrNotFound", err)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		t.Parallel()
		m, cs, code, key := setup(t)
		ctx := context.Background()
		if err := m.Delete(ctx, code); err != nil {
			t.Fatalf("Delete() error = %v, want nil", err)
		}
		if _, err := cs.Get(ctx, key); !errors.Is(err, cache.ErrNotFound) {
			t.Fatalf("cache Get() after Delete = %v, want ErrNotFound", err)
		}
	})
}
