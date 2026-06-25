package sentry_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/logger/sentry"
)

func TestNewEmptyDSNReturnsPlainLogger(t *testing.T) {
	var buf bytes.Buffer
	log, flush, err := sentry.New(sentry.WithOutput(&buf)) // DefaultConfig has empty DSN
	require.NoError(t, err)
	require.NotNil(t, log)

	log.Info("hello")
	assert.Contains(t, buf.String(), "hello")
	require.NoError(t, flush(context.Background())) // no-op flush
}

func TestNewInvalidConfigSurfacesError(t *testing.T) {
	bad := sentry.DefaultConfig()
	bad.MinLevel = "loud"
	_, flush, err := sentry.New(sentry.WithConfig(bad))
	require.Error(t, err)
	assert.ErrorIs(t, err, sentry.ErrInvalidConfig)
	// Flush is always non-nil, so `defer flush(ctx)` is safe even on a fatal config error.
	require.NotNil(t, flush)
	require.NoError(t, flush(context.Background()))
}
