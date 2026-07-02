package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/backoff"
	"github.com/dmitrymomot/forge/retry"
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
