package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/ops/config"
)

func TestProfilePredicates(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	assert.Equal(t, "production", config.Profile())
	assert.True(t, config.IsProd())
	assert.False(t, config.IsDev())

	t.Setenv("APP_ENV", "")
	t.Setenv("ENV", "")
	assert.Equal(t, "development", config.Profile()) // default
	assert.True(t, config.IsDev())
}
