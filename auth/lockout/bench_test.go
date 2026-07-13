package lockout_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func benchLocker(b *testing.B, opts ...lockout.Option) *lockout.Locker {
	b.Helper()
	counters := ratelimit.NewMemoryStore()
	b.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	b.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks, opts...)
	if err != nil {
		b.Fatal(err)
	}
	return lk
}

// The hot path: every login attempt pays one Allow.
func BenchmarkAllowUnlocked(b *testing.B) {
	lk := benchLocker(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Allow(ctx, "user@example.com"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAllowLocked(b *testing.B) {
	lk := benchLocker(b, lockout.WithThreshold(1), lockout.WithBaseLock(time.Hour), lockout.WithMaxLock(time.Hour))
	ctx := context.Background()
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Allow(ctx, "u"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFailBelowThreshold(b *testing.B) {
	lk := benchLocker(b, lockout.WithThreshold(1<<40)) // never crosses
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Fail(ctx, "u"); err != nil {
			b.Fatal(err)
		}
	}
}

// Steady-state Fail after the threshold: SetNX conflict + marker read.
func BenchmarkFailWhileLocked(b *testing.B) {
	lk := benchLocker(b, lockout.WithThreshold(1), lockout.WithBaseLock(time.Hour), lockout.WithMaxLock(time.Hour))
	ctx := context.Background()
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	if _, err := lk.Fail(ctx, "u"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := lk.Fail(ctx, "u"); err != nil {
			b.Fatal(err)
		}
	}
}
