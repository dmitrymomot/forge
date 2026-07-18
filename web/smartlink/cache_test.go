package smartlink_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/web/smartlink"
)

// countingLoad returns a load func that returns spec (or err, if non-nil) and
// counts its calls in n.
func countingLoad(n *int, spec smartlink.Spec, err error) func(context.Context, string) (smartlink.Spec, error) {
	return func(context.Context, string) (smartlink.Spec, error) {
		*n++
		if err != nil {
			return smartlink.Spec{}, err
		}
		return spec, nil
	}
}

// mustNewCache builds a Cache or fails the test.
func mustNewCache(tb testing.TB, load func(context.Context, string) (smartlink.Spec, error), opts ...smartlink.CacheOption) *smartlink.Cache {
	tb.Helper()
	c, err := smartlink.NewCache(load, opts...)
	if err != nil {
		tb.Fatalf("NewCache() error = %v", err)
	}
	return c
}

func TestNewCacheNilLoad(t *testing.T) {
	t.Parallel()
	if _, err := smartlink.NewCache(nil); err == nil {
		t.Fatal("NewCache(nil) error = nil, want construction error")
	}
}

func TestCacheGetCompilesOnce(t *testing.T) {
	t.Parallel()
	var calls int
	cache := mustNewCache(t, countingLoad(&calls, smartlink.Spec{Default: defTargets()}, nil))
	ctx := context.Background()

	if _, err := cache.Get(ctx, "ref-1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := cache.Get(ctx, "ref-1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("load calls = %d, want 1", calls)
	}
}

func TestCacheInvalidate(t *testing.T) {
	t.Parallel()
	var calls int
	cache := mustNewCache(t, countingLoad(&calls, smartlink.Spec{Default: defTargets()}, nil))
	ctx := context.Background()

	if _, err := cache.Get(ctx, "ref-1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	cache.Invalidate("ref-1")
	if _, err := cache.Get(ctx, "ref-1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("load calls = %d, want 2", calls)
	}
}

func TestCacheLoadErrorNotCached(t *testing.T) {
	t.Parallel()
	loadErr := errors.New("boundary: db unavailable")
	first := true
	cache := mustNewCache(t, func(context.Context, string) (smartlink.Spec, error) {
		if first {
			first = false
			return smartlink.Spec{}, loadErr
		}
		return smartlink.Spec{Default: defTargets()}, nil
	})
	ctx := context.Background()

	if _, err := cache.Get(ctx, "ref-1"); !errors.Is(err, loadErr) {
		t.Fatalf("Get() error = %v, want wrapping %v", err, loadErr)
	}
	if _, err := cache.Get(ctx, "ref-1"); err != nil {
		t.Fatalf("Get() error = %v, want nil on retry", err)
	}
}

func TestCacheCompileErrorPropagates(t *testing.T) {
	t.Parallel()
	// Spec{} has no default target, a Compile-time validation error.
	cache := mustNewCache(t, func(context.Context, string) (smartlink.Spec, error) {
		return smartlink.Spec{}, nil
	})
	ctx := context.Background()

	_, err := cache.Get(ctx, "ref-1")
	if !errors.Is(err, smartlink.ErrNoDefault) {
		t.Fatalf("Get() error = %v, want wrapping %v", err, smartlink.ErrNoDefault)
	}
}

// TestCacheInvalidateDuringLoad asserts that an Invalidate issued while a
// load is in flight cannot be defeated by the stale result: the straggler's
// Compiled must not overwrite a fresher entry stored after the Invalidate.
func TestCacheInvalidateDuringLoad(t *testing.T) {
	t.Parallel()
	const (
		oldURL = "https://old.example.com/"
		newURL = "https://new.example.com/"
	)
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	cache := mustNewCache(t, func(context.Context, string) (smartlink.Spec, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return smartlink.Spec{Default: []smartlink.Target{{URL: oldURL}}}, nil
		}
		return smartlink.Spec{Default: []smartlink.Target{{URL: newURL}}}, nil
	})
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// The straggler still gets a correct-at-call-time result; only the
		// store must be suppressed.
		if _, err := cache.Get(ctx, "ref-1"); err != nil {
			t.Errorf("straggler Get() error = %v", err)
		}
	}()

	<-entered // straggler is inside load, nothing cached yet
	cache.Invalidate("ref-1")

	compiled, err := cache.Get(ctx, "ref-1") // loads and stores the new Spec
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := compiled.Decide(smartlink.Visit{}).URL; got != newURL {
		t.Fatalf("post-invalidate Decide().URL = %q, want %q", got, newURL)
	}

	close(release) // straggler finishes; its stale result must be discarded
	<-done

	final, err := cache.Get(ctx, "ref-1")
	if err != nil {
		t.Fatalf("final Get() error = %v", err)
	}
	if got := final.Decide(smartlink.Visit{}).URL; got != newURL {
		t.Fatalf("final Decide().URL = %q, want %q (stale store overwrote fresh entry)", got, newURL)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("load calls = %d, want 2 (final Get must hit the cache)", n)
	}
}

// TestCacheRejectsNonPositiveTTL asserts WithRefTTL requires a positive
// duration: an unbounded entry would defeat the staleness/residency bound.
func TestCacheRejectsNonPositiveTTL(t *testing.T) {
	t.Parallel()
	load := func(context.Context, string) (smartlink.Spec, error) {
		return smartlink.Spec{Default: defTargets()}, nil
	}
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := smartlink.NewCache(load, smartlink.WithRefTTL(ttl)); err == nil {
			t.Fatalf("NewCache(WithRefTTL(%s)) error = nil, want error", ttl)
		}
	}
}

// TestCacheRefTTLExpiry asserts a cached entry is served only within its TTL:
// once the (mocked) clock passes expiry, the next Get reloads instead of
// serving the resident compile forever.
func TestCacheRefTTLExpiry(t *testing.T) {
	t.Parallel()
	mock := clock.NewMock(time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC))
	var calls int
	cache := mustNewCache(t, countingLoad(&calls, smartlink.Spec{Default: defTargets()}, nil),
		smartlink.WithRefTTL(time.Minute),
		smartlink.WithCompileOptions(smartlink.WithClock(mock)))
	ctx := context.Background()

	for range 2 {
		if _, err := cache.Get(ctx, "ref-1"); err != nil {
			t.Fatalf("Get() error = %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("load calls before expiry = %d, want 1", calls)
	}

	mock.Advance(2 * time.Minute)
	if _, err := cache.Get(ctx, "ref-1"); err != nil {
		t.Fatalf("Get() after expiry error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("load calls after expiry = %d, want 2 (expired entry must reload)", calls)
	}
}

// TestCacheGetContextBoundsWait asserts a Get whose context ends while the
// shared load is stuck returns the context error instead of waiting
// indefinitely, while the detached load still completes into the cache for
// later callers.
func TestCacheGetContextBoundsWait(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	cache := mustNewCache(t, func(context.Context, string) (smartlink.Spec, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return smartlink.Spec{Default: defTargets()}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := cache.Get(ctx, "ref-1")
		errCh <- err
	}()
	<-entered // the load is stuck; the Get is waiting on the fill
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Get() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Get() did not return after its context was canceled")
	}

	close(release) // the detached load finishes into the cache
	if _, err := cache.Get(context.Background(), "ref-1"); err != nil {
		t.Fatalf("Get() after release error = %v, want nil (fill must complete into the cache)", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("load calls = %d, want 1 (a canceled waiter must not abort or rerun the shared load)", n)
	}
}

// TestCacheLoadPanicBecomesError asserts a panicking load surfaces as an
// error to every caller — the fill runs in a detached goroutine with no
// caller to re-panic into — and is not cached, so the next Get retries.
func TestCacheLoadPanicBecomesError(t *testing.T) {
	t.Parallel()
	first := true
	cache := mustNewCache(t, func(context.Context, string) (smartlink.Spec, error) {
		if first {
			first = false
			panic("load boom")
		}
		return smartlink.Spec{Default: defTargets()}, nil
	})
	ctx := context.Background()

	if _, err := cache.Get(ctx, "ref-1"); err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("Get() error = %v, want the load panic converted to an error", err)
	}
	if _, err := cache.Get(ctx, "ref-1"); err != nil {
		t.Fatalf("Get() error = %v, want nil on retry (a panicked entry must not be cached)", err)
	}
}

func TestCacheResolver(t *testing.T) {
	t.Parallel()
	cache := mustNewCache(t, func(context.Context, string) (smartlink.Spec, error) {
		return smartlink.Spec{Default: defTargets()}, nil
	})
	resolver := cache.Resolver(tagDecorator("R"))

	d, err := resolver(context.Background(), smartlink.Link{Ref: "ref-1"})
	if err != nil {
		t.Fatalf("Resolver() error = %v", err)
	}
	got := d.Decide(smartlink.Visit{}).Rule
	if got != "R" {
		t.Fatalf("Decide().Rule = %q, want %q", got, "R")
	}
}

// TestCacheGetDedupsConcurrentLoads asserts concurrent Gets for one ref
// share a single load+compile instead of each hitting the consumer's
// database: the second Get joins the in-flight leader (or hits the completed
// entry) — either way exactly one load runs.
func TestCacheGetDedupsConcurrentLoads(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	cache := mustNewCache(t, func(context.Context, string) (smartlink.Spec, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return smartlink.Spec{Default: defTargets()}, nil
	})
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := cache.Get(ctx, "ref-1"); err != nil {
			t.Errorf("leader Get() error = %v", err)
		}
	}()
	<-entered // leader is inside load; the entry is already in the map

	joined := make(chan struct{})
	go func() {
		defer close(joined)
		if _, err := cache.Get(ctx, "ref-1"); err != nil {
			t.Errorf("joining Get() error = %v", err)
		}
	}()
	close(release)
	<-done
	<-joined

	if n := calls.Load(); n != 1 {
		t.Fatalf("load calls = %d, want 1 (concurrent misses must share one load)", n)
	}
}
