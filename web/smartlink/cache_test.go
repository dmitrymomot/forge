package smartlink_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

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
func mustNewCache(tb testing.TB, load func(context.Context, string) (smartlink.Spec, error), opts ...smartlink.Option) *smartlink.Cache {
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
