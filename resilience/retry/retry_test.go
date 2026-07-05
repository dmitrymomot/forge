package retry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/backoff"
	"github.com/dmitrymomot/forge/resilience/retry"
)

var fast = retry.WithBackoff(backoff.Constant(time.Millisecond))

func TestSucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("boom")
		}
		return nil
	}, retry.WithMaxAttempts(5), fast)
	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestExhaustsAndReturnsLastError(t *testing.T) {
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return errors.New("boom")
	}, retry.WithMaxAttempts(3), fast)
	assert.Error(t, err)
	assert.Equal(t, 3, calls)
}

func TestPermanentStopsImmediately(t *testing.T) {
	sentinel := errors.New("nope")
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return retry.Permanent(sentinel)
	}, retry.WithMaxAttempts(5), fast)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 1, calls)
}

func TestRetryIfGate(t *testing.T) {
	stop := errors.New("do-not-retry")
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return stop
	}, retry.WithMaxAttempts(5), fast, retry.WithRetryIf(func(e error) bool {
		return !errors.Is(e, stop)
	}))
	assert.ErrorIs(t, err, stop)
	assert.Equal(t, 1, calls)
}

func TestContextCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := retry.Do(ctx, func(context.Context) error {
		return errors.New("boom")
	}, retry.WithMaxAttempts(5), retry.WithBackoff(backoff.Constant(time.Hour)))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRetrierReusable(t *testing.T) {
	r := retry.New(retry.WithMaxAttempts(2), fast)
	assert.Error(t, r.Do(t.Context(), func(context.Context) error { return errors.New("x") }))
	assert.NoError(t, r.Do(t.Context(), func(context.Context) error { return nil }))
}

func TestWithMaxAttemptsZeroIgnored(t *testing.T) {
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return errors.New("boom")
	}, retry.WithMaxAttempts(0), fast) // 0 ignored -> default of 3 attempts
	assert.Error(t, err)
	assert.Equal(t, 3, calls)
}

func TestWithBackoffNilIgnored(t *testing.T) {
	// A nil backoff must be ignored (the default is kept), not stored — otherwise
	// Next would be invoked on a nil Backoff. Permanent stops after one call, so
	// no real backoff wait is incurred.
	calls := 0
	err := retry.Do(t.Context(), func(context.Context) error {
		calls++
		return retry.Permanent(errors.New("stop"))
	}, retry.WithBackoff(nil))
	assert.Error(t, err)
	assert.Equal(t, 1, calls)
}

type retryAfterErr struct{ d time.Duration }

func (e retryAfterErr) Error() string             { return "slow down" }
func (e retryAfterErr) RetryAfter() time.Duration { return e.d }

func TestRetryAfterRaisesDelayToFloor(t *testing.T) {
	var attempts int
	start := time.Now()
	err := retry.Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 2 {
			return retryAfterErr{d: 120 * time.Millisecond}
		}
		return nil
	},
		retry.WithMaxAttempts(2),
		retry.WithBackoff(backoff.Constant(1*time.Millisecond)), // far below the hint
	)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("waited %v, want >= ~120ms (the RetryAfter floor)", elapsed)
	}
}

func TestRetryAfterHonoredThroughWrappedError(t *testing.T) {
	var attempts int
	start := time.Now()
	_ = retry.Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("wrapped: %w", retryAfterErr{d: 120 * time.Millisecond})
		}
		return nil
	}, retry.WithMaxAttempts(2), retry.WithBackoff(backoff.Constant(1*time.Millisecond)))
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("waited %v, want >= ~120ms via wrapped hint", elapsed)
	}
}

func TestRetryAfterBelowBackoffKeepsBackoff(t *testing.T) {
	var attempts int
	start := time.Now()
	_ = retry.Do(context.Background(), func(context.Context) error {
		attempts++
		if attempts < 2 {
			return retryAfterErr{d: 1 * time.Millisecond} // below backoff
		}
		return nil
	}, retry.WithMaxAttempts(2), retry.WithBackoff(backoff.Constant(80*time.Millisecond)))
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("waited %v, want >= ~80ms (backoff wins)", elapsed)
	}
}
