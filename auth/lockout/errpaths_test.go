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
