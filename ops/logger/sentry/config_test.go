package sentry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/ops/logger/sentry"
)

func TestDefaultConfig(t *testing.T) {
	c := sentry.DefaultConfig()
	assert.Equal(t, "production", c.Environment)
	assert.Equal(t, "warn", c.MinLevel)
	assert.Empty(t, c.DSN)
	assert.Equal(t, "info", c.Level)  // promoted from embedded logger.Config
	assert.Equal(t, "text", c.Format) // promoted
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

func TestValidateBadPrimaryLevelMatchesBothSentinels(t *testing.T) {
	c := sentry.DefaultConfig()
	c.Level = "nope" // bad embedded logger.Config.Level
	err := c.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentry.ErrInvalidConfig)
	assert.ErrorIs(t, err, logger.ErrInvalidConfig)
}
