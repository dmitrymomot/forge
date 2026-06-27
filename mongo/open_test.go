package mongo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/mongo"
)

func TestOpen_RetryExhausted(t *testing.T) {
	// Point at an unreachable address with a tiny server-selection timeout so each
	// ping fails fast. With RetryAttempts=2 and a tiny interval the loop exhausts
	// quickly and returns ErrConnect (not ErrInvalidConfig). Bounded by a generous
	// test timeout to catch a hung loop.
	cfg := forgemongo.DefaultConfig()
	cfg.URI = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100"
	cfg.RetryAttempts = 2
	cfg.RetryInterval = 10 * time.Millisecond

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg))
		assert.Nil(t, c)
		require.Error(t, err)
		assert.ErrorIs(t, err, forgemongo.ErrConnect)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Open did not return; retry loop likely hung")
	}
}

func TestOpen_ContextCancelled(t *testing.T) {
	// A pre-cancelled context aborts the retry loop promptly with a wrapped ErrConnect.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cfg := forgemongo.DefaultConfig()
	cfg.URI = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=100"
	cfg.RetryAttempts = 5
	cfg.RetryInterval = time.Second

	c, err := forgemongo.Open(ctx, forgemongo.WithConfig(cfg))
	assert.Nil(t, c)
	require.Error(t, err)
	assert.ErrorIs(t, err, forgemongo.ErrConnect)
}
