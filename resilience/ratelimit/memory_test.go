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

func TestMemoryStore_IncrGetResetExpiry(t *testing.T) {
	mk := clock.NewMock(time.Unix(1000, 0))
	s := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk))
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	n, err := s.Incr(ctx, "k", 1, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = s.Incr(ctx, "k", 2, time.Minute) // does not extend TTL
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	got, err := s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(3), got)

	mk.Advance(61 * time.Second) // past TTL
	got, err = s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got) // expired

	got, _ = s.Get(ctx, "absent")
	assert.Equal(t, int64(0), got)
}

func TestMemoryStore_ConcurrentIncr(t *testing.T) {
	s := ratelimit.NewMemoryStore()
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	const g = 100
	done := make(chan struct{}, g)
	for range g {
		go func() { _, _ = s.Incr(ctx, "c", 1, time.Minute); done <- struct{}{} }()
	}
	for range g {
		<-done
	}
	n, _ := s.Get(ctx, "c")
	assert.Equal(t, int64(g), n)
}
