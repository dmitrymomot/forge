package lockout_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

var hashSuffix = regexp.MustCompile(`^[0-9a-f]{32}$`)

// recordingCounters/recordingLocks capture the composed store keys so the
// fuzz target can assert key-shape invariants black-box.
type recordingCounters struct {
	ratelimit.Store
	keys *[]string
}

func (r recordingCounters) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	*r.keys = append(*r.keys, key)
	return r.Store.Incr(ctx, key, delta, ttl)
}

func (r recordingCounters) Get(ctx context.Context, key string) (int64, error) {
	*r.keys = append(*r.keys, key)
	return r.Store.Get(ctx, key)
}

type recordingLocks struct {
	cache.Store
	keys *[]string
}

func (r recordingLocks) Get(ctx context.Context, key string) ([]byte, error) {
	*r.keys = append(*r.keys, key)
	return r.Store.Get(ctx, key)
}

func FuzzKeyComposition(f *testing.F) {
	f.Add("user@example.com", "", false)
	f.Add("user@example.com", "tenant-a", true)
	f.Add("", "t", true)
	f.Add("key:with:colons", "scope:with:colons", true)
	f.Add(strings.Repeat("x", 10_000), "t", true)
	f.Add("\x00\xff unicode ⚡", "теnant", true)

	f.Fuzz(func(t *testing.T, key, scope string, scoped bool) {
		var counterKeys, lockKeys []string
		counters := ratelimit.NewMemoryStore()
		defer func() { _ = counters.Close() }()
		locks := cache.NewMemoryStore()
		defer func() { _ = locks.Close() }()

		opts := []lockout.Option{lockout.WithThreshold(2)}
		if scoped {
			opts = append(opts, lockout.WithScope(func(context.Context) (string, error) {
				return scope, nil
			}))
		}
		lk, err := lockout.New(
			recordingCounters{Store: counters, keys: &counterKeys},
			recordingLocks{Store: locks, keys: &lockKeys},
			opts...,
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		ctx := context.Background()
		res, err := lk.Fail(ctx, key)
		if scoped && scope == "" {
			if !errors.Is(err, lockout.ErrScope) {
				t.Fatalf("empty scope must fail closed, got %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("Fail: %v", err)
		}
		if res.Failures != 1 {
			t.Fatalf("Failures = %d, want 1", res.Failures)
		}

		out, err := lk.Allow(ctx, key)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if out.Failures != 1 {
			t.Fatalf("round-trip Failures = %d, want 1", out.Failures)
		}

		wantPrefix := "lockout:"
		if scoped {
			wantPrefix += scope + ":"
		}
		for _, k := range counterKeys {
			rest, ok := strings.CutPrefix(k, wantPrefix+"f:")
			if !ok || !hashSuffix.MatchString(rest) {
				t.Fatalf("counter key %q lacks shape %sf:<32 hex>", k, wantPrefix)
			}
		}
		for _, k := range lockKeys {
			rest, ok := strings.CutPrefix(k, wantPrefix+"l:")
			if !ok || !hashSuffix.MatchString(rest) {
				t.Fatalf("lock key %q lacks shape %sl:<32 hex>", k, wantPrefix)
			}
		}
		if counterKeys[0] == lockKeys[0] {
			t.Fatalf("counter and lock keys must differ: %q", counterKeys[0])
		}
	})
}
