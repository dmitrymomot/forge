package sentry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger/sentry"
)

func TestDefaultConfig(t *testing.T) {
	c := sentry.DefaultConfig()
	assert.Equal(t, "production", c.Environment)
	assert.Equal(t, "warn", c.MinLevel)
	assert.Empty(t, c.DSN)
	assert.False(t, c.EnableLogs)
	assert.False(t, c.AddSource)
}

func TestValidateGoodAndWarningAlias(t *testing.T) {
	require.NoError(t, sentry.DefaultConfig().Validate())
	c := sentry.DefaultConfig()
	c.MinLevel = "WARNING"
	require.NoError(t, c.Validate())
}

func TestValidateBadMinLevel(t *testing.T) {
	c := sentry.DefaultConfig()
	c.MinLevel = "loud"
	err := c.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentry.ErrInvalidConfig)
}
