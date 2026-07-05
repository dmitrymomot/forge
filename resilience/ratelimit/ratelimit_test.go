package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

func TestLimiter_AllowsUpToLimit(t *testing.T) {
	mk := clock.NewMock(time.Unix(0, 0))
	l := ratelimit.New(
		ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk)),
		ratelimit.WithLimit(3, time.Minute),
		ratelimit.WithClock(mk),
	)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		res, err := l.Allow(ctx, "user")
		require.NoError(t, err)
		assert.True(t, res.Allowed, "request %d should pass", i)
		assert.Equal(t, int64(3), res.Limit)
	}
	res, err := l.Allow(ctx, "user")
	require.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.Equal(t, int64(0), res.Remaining)
	assert.Positive(t, res.RetryAfter)
}

func TestLimiter_KeysAreIsolated(t *testing.T) {
	l := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.WithLimit(1, time.Minute))
	ctx := context.Background()
	a, _ := l.Allow(ctx, "a")
	b, _ := l.Allow(ctx, "b")
	assert.True(t, a.Allowed)
	assert.True(t, b.Allowed)
}
