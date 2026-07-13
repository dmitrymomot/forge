package lockout_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func TestLockedErrorErrorString(t *testing.T) {
	t.Parallel()

	bare := &lockout.LockedError{}
	require.Equal(t, "lockout: locked", bare.Error())
	require.Equal(t, lockout.ErrLocked.Error(), bare.Error())

	wrapped := &lockout.LockedError{Err: errors.New("bad password")}
	require.Equal(t, "lockout: locked: bad password", wrapped.Error())
}

func TestLockStateMalformedMarker(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })

	lk, err := lockout.New(counters, garbageLocks{})
	require.NoError(t, err)

	_, err = lk.Allow(context.Background(), "u")
	require.ErrorIs(t, err, lockout.ErrStore)
}

type garbageLocks struct{}

func (garbageLocks) Get(context.Context, string) ([]byte, error) { return []byte("not-a-number"), nil }
func (garbageLocks) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return nil
}
func (garbageLocks) Delete(context.Context, string) error       { return nil }
func (garbageLocks) Has(context.Context, string) (bool, error)  { return false, nil }
func (garbageLocks) DeletePrefix(context.Context, string) error { return nil }
func (garbageLocks) Close() error                               { return nil }

func TestResetCounterOnlyFails(t *testing.T) {
	t.Parallel()
	inner := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = inner.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })

	lk, err := lockout.New(resetFailingCounters{inner: inner}, locks)
	require.NoError(t, err)

	rerr := lk.Reset(context.Background(), "u")
	require.ErrorIs(t, rerr, lockout.ErrStore)
	require.NotContains(t, rerr.Error(), "\n") //nolint:nilaway // rerr is guaranteed non-nil by require.ErrorIs above
}

func TestResetLockOnlyFails(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })

	lk, err := lockout.New(counters, deleteFailingLocks{})
	require.NoError(t, err)

	rerr := lk.Reset(context.Background(), "u")
	require.ErrorIs(t, rerr, lockout.ErrStore)
	require.NotContains(t, rerr.Error(), "\n") //nolint:nilaway // rerr is guaranteed non-nil by require.ErrorIs above
}

type deleteFailingLocks struct{}

func (deleteFailingLocks) Get(context.Context, string) ([]byte, error) {
	return nil, cache.ErrNotFound
}
func (deleteFailingLocks) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return nil
}
func (deleteFailingLocks) Delete(context.Context, string) error       { return errBoom }
func (deleteFailingLocks) Has(context.Context, string) (bool, error)  { return false, nil }
func (deleteFailingLocks) DeletePrefix(context.Context, string) error { return nil }
func (deleteFailingLocks) Close() error                               { return nil }

func TestDoFailStoreErrorPreservesBothChains(t *testing.T) {
	t.Parallel()
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })

	lk, err := lockout.New(incrFailingCounters{}, locks)
	require.NoError(t, err)

	derr := lk.Do(context.Background(), "u", func(context.Context) error { return lockout.ErrFailedAttempt })
	require.ErrorIs(t, derr, lockout.ErrFailedAttempt)
	require.ErrorIs(t, derr, lockout.ErrStore)
	require.NotContains(t, derr.Error(), "\n") //nolint:nilaway // derr is guaranteed non-nil by require.ErrorIs above
}

type incrFailingCounters struct{}

func (incrFailingCounters) Incr(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errBoom
}
func (incrFailingCounters) Get(context.Context, string) (int64, error) { return 0, nil }
func (incrFailingCounters) Reset(context.Context, string) error        { return nil }
func (incrFailingCounters) Close() error                               { return nil }

func TestDoPropagatesAllowError(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(failingCounters{}, failingLocks{})
	require.NoError(t, err)

	var ran bool
	derr := lk.Do(context.Background(), "u", func(context.Context) error {
		ran = true
		return nil
	})
	require.ErrorIs(t, derr, lockout.ErrStore)
	require.False(t, ran, "fn must not run when Allow fails on the lock read")
}

func TestAllowCounterGetFails(t *testing.T) {
	t.Parallel()
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })

	lk, err := lockout.New(getFailingCounters{}, locks)
	require.NoError(t, err)

	_, aerr := lk.Allow(context.Background(), "u")
	require.ErrorIs(t, aerr, lockout.ErrStore)
}

type getFailingCounters struct{}

func (getFailingCounters) Incr(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, nil
}
func (getFailingCounters) Get(context.Context, string) (int64, error) { return 0, errBoom }
func (getFailingCounters) Reset(context.Context, string) error        { return nil }
func (getFailingCounters) Close() error                               { return nil }

func TestFailSetNonExistErrorWraps(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(ratelimit.NewMemoryStore(), setFailingLocks{}, lockout.WithThreshold(1))
	require.NoError(t, err)
	ctx := context.Background()

	_, err = lk.Fail(ctx, "u") // n=1: below threshold, no Set call
	require.NoError(t, err)

	_, ferr := lk.Fail(ctx, "u") // n=2: crosses threshold, Set fails with a non-ErrExists error
	require.ErrorIs(t, ferr, lockout.ErrStore)
}

type setFailingLocks struct{}

func (setFailingLocks) Get(context.Context, string) ([]byte, error) { return nil, cache.ErrNotFound }
func (setFailingLocks) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return errBoom
}
func (setFailingLocks) Delete(context.Context, string) error       { return nil }
func (setFailingLocks) Has(context.Context, string) (bool, error)  { return false, nil }
func (setFailingLocks) DeletePrefix(context.Context, string) error { return nil }
func (setFailingLocks) Close() error                               { return nil }

func TestFailSetNXLoserMarkerExpired(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(ratelimit.NewMemoryStore(), errExistsExpiredLocks{}, lockout.WithThreshold(1))
	require.NoError(t, err)
	ctx := context.Background()

	_, err = lk.Fail(ctx, "u") // n=1: below threshold
	require.NoError(t, err)

	res, err := lk.Fail(ctx, "u") // n=2: crosses, SetNX loses, marker already gone
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 2, res.Failures)
}

type errExistsExpiredLocks struct{}

func (errExistsExpiredLocks) Get(context.Context, string) ([]byte, error) {
	return nil, cache.ErrNotFound
}
func (errExistsExpiredLocks) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return cache.ErrExists
}
func (errExistsExpiredLocks) Delete(context.Context, string) error       { return nil }
func (errExistsExpiredLocks) Has(context.Context, string) (bool, error)  { return false, nil }
func (errExistsExpiredLocks) DeletePrefix(context.Context, string) error { return nil }
func (errExistsExpiredLocks) Close() error                               { return nil }

func TestFailSetNXLoserLockStateErrors(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(ratelimit.NewMemoryStore(), errExistsGetErrLocks{}, lockout.WithThreshold(1))
	require.NoError(t, err)
	ctx := context.Background()

	_, err = lk.Fail(ctx, "u") // n=1: below threshold
	require.NoError(t, err)

	_, ferr := lk.Fail(ctx, "u") // n=2: crosses, SetNX loses, re-read errors
	require.ErrorIs(t, ferr, lockout.ErrStore)
}

type errExistsGetErrLocks struct{}

func (errExistsGetErrLocks) Get(context.Context, string) ([]byte, error) { return nil, errBoom }
func (errExistsGetErrLocks) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return cache.ErrExists
}
func (errExistsGetErrLocks) Delete(context.Context, string) error       { return nil }
func (errExistsGetErrLocks) Has(context.Context, string) (bool, error)  { return false, nil }
func (errExistsGetErrLocks) DeletePrefix(context.Context, string) error { return nil }
func (errExistsGetErrLocks) Close() error                               { return nil }
