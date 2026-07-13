package lockout_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func TestDoSuccessResets(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(2))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)

	require.NoError(t, lk.Do(ctx, "u", func(context.Context) error { return nil }))

	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.EqualValues(t, 0, res.Failures) // success cleared the slate
}

func TestDoFailedAttemptCounted(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3))
	ctx := context.Background()

	wrapped := fmt.Errorf("bad password: %w", lockout.ErrFailedAttempt)
	err := lk.Do(ctx, "u", func(context.Context) error { return wrapped })
	require.ErrorIs(t, err, lockout.ErrFailedAttempt)
	require.NotErrorIs(t, err, lockout.ErrLocked)
	require.Equal(t, wrapped, err) // below threshold: fn error passes through unchanged

	res, aerr := lk.Allow(ctx, "u")
	require.NoError(t, aerr)
	require.EqualValues(t, 1, res.Failures)
}

func TestDoCrossingThresholdReturnsLockedError(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u") // n=1: at threshold, next failure locks
	require.NoError(t, err)

	err = lk.Do(ctx, "u", func(context.Context) error { return lockout.ErrFailedAttempt })
	require.ErrorIs(t, err, lockout.ErrLocked)
	require.ErrorIs(t, err, lockout.ErrFailedAttempt)

	var le *lockout.LockedError
	require.ErrorAs(t, err, &le)
	require.True(t, le.Result.Locked)                   //nolint:nilaway // le is guaranteed non-nil by require.ErrorAs above
	require.Equal(t, time.Minute, le.Result.RetryAfter) //nolint:nilaway // le is guaranteed non-nil by require.ErrorAs above
	require.EqualValues(t, 2, le.Result.Failures)       //nolint:nilaway // le is guaranteed non-nil by require.ErrorAs above
}

func TestDoLockedOnEntry(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u") // locked for 1m
	require.NoError(t, err)

	ran := false
	err = lk.Do(ctx, "u", func(context.Context) error { ran = true; return nil })
	require.ErrorIs(t, err, lockout.ErrLocked)
	require.NotErrorIs(t, err, lockout.ErrFailedAttempt)
	require.False(t, ran, "fn must not run while locked")

	var le *lockout.LockedError
	require.ErrorAs(t, err, &le)
	require.Positive(t, le.Result.RetryAfter) //nolint:nilaway // le is guaranteed non-nil by require.ErrorAs above
}

func TestDoInfraErrorPassthroughUncounted(t *testing.T) {
	t.Parallel()
	lk := newLocker(t)
	ctx := context.Background()

	infra := errors.New("db down")
	err := lk.Do(ctx, "u", func(context.Context) error { return infra })
	require.Equal(t, infra, err) // untouched
	require.NotErrorIs(t, err, lockout.ErrLocked)

	res, aerr := lk.Allow(ctx, "u")
	require.NoError(t, aerr)
	require.EqualValues(t, 0, res.Failures) // not counted
}

func TestDoResetFailureSurfaced(t *testing.T) {
	t.Parallel()
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(resetFailingCounters{inner: ratelimit.NewMemoryStore()}, locks)
	require.NoError(t, err)

	err = lk.Do(context.Background(), "u", func(context.Context) error { return nil })
	require.ErrorIs(t, err, lockout.ErrStore) // reset failure is never swallowed
}

type resetFailingCounters struct{ inner ratelimit.Store }

func (s resetFailingCounters) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	return s.inner.Incr(ctx, key, delta, ttl)
}
func (s resetFailingCounters) Get(ctx context.Context, key string) (int64, error) {
	return s.inner.Get(ctx, key)
}
func (s resetFailingCounters) Reset(context.Context, string) error {
	return errors.New("reset boom")
}
func (s resetFailingCounters) Close() error { return s.inner.Close() }
