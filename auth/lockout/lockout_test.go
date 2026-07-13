package lockout_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/lockout"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func newLocker(t *testing.T, opts ...lockout.Option) *lockout.Locker {
	t.Helper()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })
	lk, err := lockout.New(counters, locks, opts...)
	require.NoError(t, err)
	return lk
}

func TestNewValidation(t *testing.T) {
	t.Parallel()
	counters := ratelimit.NewMemoryStore()
	t.Cleanup(func() { _ = counters.Close() })
	locks := cache.NewMemoryStore()
	t.Cleanup(func() { _ = locks.Close() })

	tests := []struct {
		name     string
		counters ratelimit.Store
		locks    cache.Store
		opts     []lockout.Option
	}{
		{"nil counters", nil, locks, nil},
		{"nil locks", counters, nil, nil},
		{"zero threshold", counters, locks, []lockout.Option{lockout.WithThreshold(0)}},
		{"zero base lock", counters, locks, []lockout.Option{lockout.WithBaseLock(0)}},
		{"factor below one", counters, locks, []lockout.Option{lockout.WithFactor(0.5)}},
		{"max below base", counters, locks, []lockout.Option{lockout.WithBaseLock(time.Hour), lockout.WithMaxLock(time.Minute)}},
		{"zero window", counters, locks, []lockout.Option{lockout.WithWindow(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := lockout.New(tt.counters, tt.locks, tt.opts...)
			require.Error(t, err)
		})
	}
}

func TestAllowCleanSlate(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3))
	res, err := lk.Allow(context.Background(), "user@example.com")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.Zero(t, res.RetryAfter)
	require.True(t, res.Until.IsZero())
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 3, res.Remaining)
}

func TestFailBelowThreshold(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3))
	ctx := context.Background()

	res, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 1, res.Failures)
	require.EqualValues(t, 2, res.Remaining)

	res, err = lk.Fail(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 2, res.Failures)
	require.EqualValues(t, 1, res.Remaining)
}

// Escalation asserted through the winner-path Result.RetryAfter, which is the
// computed duration exactly (deterministic even on the system clock). Real
// short TTLs let each lock expire so the next Fail creates the next lock.
func TestFailEscalationProgression(t *testing.T) {
	t.Parallel()
	lk := newLocker(t,
		lockout.WithThreshold(1),
		lockout.WithBaseLock(40*time.Millisecond),
		lockout.WithFactor(2),
		lockout.WithMaxLock(200*time.Millisecond),
		lockout.WithWindow(5*time.Second),
	)
	ctx := context.Background()

	res, err := lk.Fail(ctx, "u") // n=1: free
	require.NoError(t, err)
	require.False(t, res.Locked)

	steps := []time.Duration{
		40 * time.Millisecond,  // n=2: base × 2^0
		80 * time.Millisecond,  // n=3: base × 2^1
		160 * time.Millisecond, // n=4: base × 2^2
		200 * time.Millisecond, // n=5: base × 2^3 = 320ms → capped
		200 * time.Millisecond, // n=6: stays capped
	}
	for _, want := range steps {
		res, err = lk.Fail(ctx, "u")
		require.NoError(t, err)
		require.True(t, res.Locked)
		require.Equal(t, want, res.RetryAfter)
		require.False(t, res.Until.IsZero())
		time.Sleep(want + 60*time.Millisecond) // let the marker expire
	}
}

func TestFailFactorOneFixedLocks(t *testing.T) {
	t.Parallel()
	lk := newLocker(t,
		lockout.WithThreshold(1),
		lockout.WithBaseLock(40*time.Millisecond),
		lockout.WithFactor(1),
		lockout.WithMaxLock(40*time.Millisecond),
		lockout.WithWindow(5*time.Second),
	)
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	for range 3 {
		res, err := lk.Fail(ctx, "u")
		require.NoError(t, err)
		require.True(t, res.Locked)
		require.Equal(t, 40*time.Millisecond, res.RetryAfter)
		time.Sleep(100 * time.Millisecond)
	}
}

func TestFailWhileLockedDoesNotExtend(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1)) // base 1m: lock outlives the test
	ctx := context.Background()

	_, err := lk.Fail(ctx, "u") // n=1: free
	require.NoError(t, err)
	first, err := lk.Fail(ctx, "u") // n=2: locks for 1m
	require.NoError(t, err)
	require.True(t, first.Locked)
	require.EqualValues(t, 2, first.Failures)

	again, err := lk.Fail(ctx, "u") // n=3: counted, lock unchanged
	require.NoError(t, err)
	require.True(t, again.Locked)
	require.EqualValues(t, 3, again.Failures)
	require.Equal(t, first.Until.UnixMilli(), again.Until.UnixMilli())
	require.LessOrEqual(t, again.RetryAfter, first.RetryAfter)
}

func TestAllowLocked(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u")
	require.NoError(t, err)

	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.True(t, res.Locked)
	require.Positive(t, res.RetryAfter)
	require.LessOrEqual(t, res.RetryAfter, time.Minute)
	// Locked path skips the counter read: Failures/Remaining stay zero.
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 0, res.Remaining)
}

// A marker whose embedded expiry has passed is treated as unlocked even if
// the store TTL has not fired yet (cross-node clock skew edge).
func TestAllowSkewedMarkerUnlocked(t *testing.T) {
	t.Parallel()
	mock := clock.NewMock(time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC))
	lk := newLocker(t, lockout.WithThreshold(1), lockout.WithClock(mock))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u") // locks until mock-now + 1m; real store TTL 1m
	require.NoError(t, err)

	mock.Advance(2 * time.Minute) // past the embedded expiry, store TTL still live
	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 2, res.Failures)
}

func TestLockExpiryUnlocks(t *testing.T) {
	t.Parallel()
	lk := newLocker(t,
		lockout.WithThreshold(1),
		lockout.WithBaseLock(100*time.Millisecond),
		lockout.WithMaxLock(100*time.Millisecond),
		lockout.WithWindow(5*time.Second),
	)
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	res, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	require.True(t, res.Locked)

	time.Sleep(250 * time.Millisecond)
	out, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.False(t, out.Locked)
	require.EqualValues(t, 2, out.Failures) // window still remembers
}

func TestWindowExpiryForgets(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(3), lockout.WithWindow(100*time.Millisecond))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u")
	require.NoError(t, err)

	time.Sleep(250 * time.Millisecond)
	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 3, res.Remaining)
}

func TestResetClearsBoth(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "u")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "u")
	require.NoError(t, err)

	require.NoError(t, lk.Reset(ctx, "u"))
	res, err := lk.Allow(ctx, "u")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 0, res.Failures)
	require.EqualValues(t, 1, res.Remaining)
}

func TestConcurrentThresholdCross(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1)) // base 1m: no expiry mid-test
	ctx := context.Background()

	const workers = 20
	results := make([]lockout.Result, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			results[i], errs[i] = lk.Fail(ctx, "u")
		})
	}
	wg.Wait()

	var locked int
	var until int64
	for i := range workers {
		require.NoError(t, errs[i])
		if !results[i].Locked {
			continue // the single n=1 free failure
		}
		locked++
		require.Positive(t, results[i].RetryAfter)
		if until == 0 {
			until = results[i].Until.UnixMilli()
		}
		require.Equal(t, until, results[i].Until.UnixMilli(), "all lockers must agree on the winner's expiry")
	}
	require.Equal(t, workers-1, locked)
}

func TestKeysAreIndependent(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, lockout.WithThreshold(1))
	ctx := context.Background()
	_, err := lk.Fail(ctx, "a@example.com")
	require.NoError(t, err)
	_, err = lk.Fail(ctx, "a@example.com")
	require.NoError(t, err)

	res, err := lk.Allow(ctx, "b@example.com")
	require.NoError(t, err)
	require.False(t, res.Locked)
	require.EqualValues(t, 0, res.Failures)
}

func TestStoreErrorWrapsErrStore(t *testing.T) {
	t.Parallel()
	lk, err := lockout.New(failingCounters{}, failingLocks{})
	require.NoError(t, err)
	ctx := context.Background()

	_, aerr := lk.Allow(ctx, "u")
	require.ErrorIs(t, aerr, lockout.ErrStore)
	_, ferr := lk.Fail(ctx, "u")
	require.ErrorIs(t, ferr, lockout.ErrStore)
	require.ErrorIs(t, lk.Reset(ctx, "u"), lockout.ErrStore)
}

var errBoom = errors.New("boom")

type failingCounters struct{}

func (failingCounters) Incr(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errBoom
}
func (failingCounters) Get(context.Context, string) (int64, error) { return 0, errBoom }
func (failingCounters) Reset(context.Context, string) error        { return errBoom }
func (failingCounters) Close() error                               { return nil }

type failingLocks struct{}

func (failingLocks) Get(context.Context, string) ([]byte, error) { return nil, errBoom }
func (failingLocks) Set(context.Context, string, []byte, ...cache.SetOption) error {
	return errBoom
}
func (failingLocks) Delete(context.Context, string) error       { return errBoom }
func (failingLocks) Has(context.Context, string) (bool, error)  { return false, errBoom }
func (failingLocks) DeletePrefix(context.Context, string) error { return errBoom }
func (failingLocks) Close() error                               { return nil }
