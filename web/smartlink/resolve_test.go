package smartlink_test

import (
	"context"
	"errors"
	"sync"
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

// TestResolveCorruptCacheFallsThrough asserts a cache entry that fails to
// decode (garbage bytes, not the Link JSON envelope) degrades identically to
// a miss: Resolve still returns the Link from the backing Store instead of
// surfacing a decode error.
func TestResolveCorruptCacheFallsThrough(t *testing.T) {
	t.Parallel()
	cs := cache.NewMemoryStore()
	m, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithCache(cs, time.Minute))
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	ctx := context.Background()

	created, err := m.Create(ctx, smartlink.CreateParams{Target: "https://example.com/", Code: "corrupt1"})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if err := cs.Set(ctx, "smartlink:code:"+created.Code, []byte("not json"), cache.WithTTL(time.Minute)); err != nil {
		t.Fatalf("cache Set() error = %v, want nil", err)
	}

	got, err := m.Resolve(ctx, created.Code)
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil (corrupt cache entry must degrade to Store)", err)
	}
	if got.Code != created.Code {
		t.Fatalf("Resolve().Code = %q, want %q", got.Code, created.Code)
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

// gateStore counts Gets and, for armCode, stalls the FIRST Get after the
// underlying read has completed — simulating an in-flight lookup that read
// the Store but has not yet written the cache.
type gateStore struct {
	smartlink.Store
	entered chan struct{}
	release chan struct{}
	armCode string
	gets    atomic.Int32
	once    sync.Once
}

func (s *gateStore) Get(ctx context.Context, code string) (smartlink.Link, error) {
	l, err := s.Store.Get(ctx, code)
	s.gets.Add(1)
	if code == s.armCode {
		s.once.Do(func() {
			close(s.entered)
			<-s.release
		})
	}
	return l, err
}

// TestResolveStaleWriteBackEvicted reproduces the delete/recreate race: an
// in-flight Resolve reads the old record, the code is Deleted and re-Created
// with a new target while that read is stalled, and the straggler's cache
// write-back must not survive — otherwise the recreated code would serve the
// previous owner's target for a full TTL.
func TestResolveStaleWriteBackEvicted(t *testing.T) {
	t.Parallel()
	const code = "reuse1"
	gs := &gateStore{
		Store:   smartlink.NewMemoryStore(),
		armCode: code,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	cs := cache.NewMemoryStore()
	m, err := smartlink.NewManager(gs, smartlink.WithCache(cs, time.Minute))
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	ctx := context.Background()

	if _, err := m.Create(ctx, smartlink.CreateParams{Code: code, Target: "https://old.example.com/"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// The straggler read the old record; its result is correct for when
		// the call began — only the cache write-back must be suppressed.
		if _, err := m.Resolve(ctx, code); err != nil {
			t.Errorf("straggler Resolve() error = %v", err)
		}
	}()

	<-gs.entered // straggler holds the old record, cache still empty
	if err := m.Delete(ctx, code); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}
	if _, err := m.Create(ctx, smartlink.CreateParams{Code: code, Target: "https://new.example.com/"}); err != nil {
		t.Fatalf("Create() (code reuse) error = %v, want nil", err)
	}
	close(gs.release)
	<-done

	if _, err := cs.Get(ctx, "smartlink:code:"+code); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("cache Get() after raced fill = %v, want ErrNotFound (stale write-back must be evicted)", err)
	}
	got, err := m.Resolve(ctx, code)
	if err != nil {
		t.Fatalf("Resolve() after reuse error = %v, want nil", err)
	}
	if got.Target != "https://new.example.com/" {
		t.Fatalf("Resolve().Target = %q, want the recreated link's target", got.Target)
	}
}

// TestResolveMissSingleflight asserts concurrent cache misses for one code
// share a single Store read instead of stampeding it: a leader Resolve is
// parked inside Store.Get, the other workers are started while it is parked
// (so their flight.Do joins the leader's registered call), and the total
// Store reads must stay 1.
func TestResolveMissSingleflight(t *testing.T) {
	t.Parallel()
	const code, workers = "hot1", 3
	gs := &gateStore{
		Store:   smartlink.NewMemoryStore(),
		armCode: code,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	m, err := smartlink.NewManager(gs, smartlink.WithCache(cache.NewMemoryStore(), time.Minute))
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	ctx := context.Background()
	if _, err := m.Create(ctx, smartlink.CreateParams{Code: code, Target: "https://example.com/"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { // leader: misses cache, parks inside Store.Get
		if _, err := m.Resolve(ctx, code); err != nil {
			t.Errorf("leader Resolve() error = %v", err)
		}
	})
	<-gs.entered
	for range workers {
		wg.Go(func() {
			if _, err := m.Resolve(ctx, code); err != nil {
				t.Errorf("Resolve() error = %v", err)
			}
		})
	}
	// The workers' path from cache miss to flight join is a handful of
	// non-blocking statements; this generous scheduling window lets them
	// reach it while the leader is still parked, keeping the assertion
	// deterministic in practice.
	time.Sleep(100 * time.Millisecond)
	close(gs.release)
	wg.Wait()
	if got := gs.gets.Load(); got != 1 {
		t.Fatalf("store Gets under concurrent miss = %d, want 1 (singleflight)", got)
	}
}

// TestResolveMissWaitBoundedByContext asserts a coalesced cache-miss Resolve
// whose context ends while the shared Store read is stuck returns the context
// error instead of waiting indefinitely — a wedged Store must not pin
// redirect requests past their deadlines.
func TestResolveMissWaitBoundedByContext(t *testing.T) {
	t.Parallel()
	const code = "stuck1"
	gs := &gateStore{
		Store:   smartlink.NewMemoryStore(),
		armCode: code,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	m, err := smartlink.NewManager(gs, smartlink.WithCache(cache.NewMemoryStore(), time.Minute))
	if err != nil {
		t.Fatalf("NewManager() error = %v, want nil", err)
	}
	if _, err := m.Create(context.Background(), smartlink.CreateParams{Code: code, Target: "https://example.com/"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { // leader: misses the cache, parks inside Store.Get
		if _, err := m.Resolve(context.Background(), code); err != nil {
			t.Errorf("leader Resolve() error = %v", err)
		}
	})
	<-gs.entered // the coalesced Store read is registered and stuck

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Resolve(ctx, code); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve() with canceled ctx = %v, want context.Canceled", err)
	}

	close(gs.release)
	wg.Wait()
	if _, err := m.Resolve(context.Background(), code); err != nil {
		t.Fatalf("Resolve() after release error = %v, want nil", err)
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
