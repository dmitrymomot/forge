package sentry_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/ops/logger/sentry"
)

func TestNewHandlerComposesWithLogger(t *testing.T) {
	h, fl, err := sentry.NewHandler() // empty DSN → disabled handler, no error
	require.NoError(t, err)

	var buf bytes.Buffer
	log, err := logger.New(logger.WithOutput(&buf), logger.WithHandler(h))
	require.NoError(t, err)

	log.Info("hello")
	assert.Contains(t, buf.String(), "hello") // primary unaffected by the disabled extra
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerInvalidConfigFlushSafe(t *testing.T) {
	bad := sentry.DefaultConfig()
	bad.MinLevel = "loud"
	_, fl, err := sentry.NewHandler(sentry.WithConfig(bad))
	require.Error(t, err)
	assert.ErrorIs(t, err, sentry.ErrInvalidConfig)
	// Flush is always non-nil, so `defer fl(ctx)` is safe even on a config error.
	require.NotNil(t, fl)
	require.NoError(t, fl(context.Background()))
}
