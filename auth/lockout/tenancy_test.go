package lockout_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

var tenantKey = ctxkey.New[string]("tenant")

func scopeFromCtx(ctx context.Context) (string, error) {
	s, _ := tenantKey.From(ctx)
	return s, nil // empty when absent → Locker fails closed
}

func newScopedLocker(t *testing.T) *lockout.Locker {
	t.Helper()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks,
		lockout.WithThreshold(1), lockout.WithScope(scopeFromCtx))
	require.NoError(t, err)
	return lk
}

func TestScopeIsolation(t *testing.T) {
	t.Parallel()
	lk := newScopedLocker(t)
	ctxA := tenantKey.With(context.Background(), "tenant-a")
	ctxB := tenantKey.With(context.Background(), "tenant-b")

	_, err := lk.Fail(ctxA, "user@example.com")
	require.NoError(t, err)
	resA, err := lk.Fail(ctxA, "user@example.com")
	require.NoError(t, err)
	require.True(t, resA.Locked)

	resB, err := lk.Allow(ctxB, "user@example.com")
	require.NoError(t, err)
	require.False(t, resB.Locked)
	require.EqualValues(t, 0, resB.Failures)
}

func TestScopeFailClosed(t *testing.T) {
	t.Parallel()
	lk := newScopedLocker(t)
	ctx := context.Background() // no tenant in context → empty scope

	_, err := lk.Allow(ctx, "u")
	require.ErrorIs(t, err, lockout.ErrScope)
	_, err = lk.Fail(ctx, "u")
	require.ErrorIs(t, err, lockout.ErrScope)
	require.ErrorIs(t, lk.Reset(ctx, "u"), lockout.ErrScope)
}

func TestScopeHookError(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	hookErr := errors.New("no tenant")
	lk, err := lockout.New(counters, locks,
		lockout.WithScope(func(context.Context) (string, error) { return "", hookErr }))
	require.NoError(t, err)

	_, aerr := lk.Allow(context.Background(), "u")
	require.ErrorIs(t, aerr, lockout.ErrScope)
	require.ErrorIs(t, aerr, hookErr)
}

func TestUnscopedSingleTenant(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks, lockout.WithThreshold(1))
	require.NoError(t, err)

	// No WithScope: plain context works, zero ceremony.
	res, err := lk.Fail(context.Background(), "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
}
