package scheduler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/scheduler"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	t.Run("defaults valid", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, scheduler.DefaultConfig().Validate())
	})

	t.Run("zero retention", func(t *testing.T) {
		t.Parallel()
		cfg := scheduler.DefaultConfig()
		cfg.Retention = 0
		assert.ErrorIs(t, cfg.Validate(), scheduler.ErrInvalidConfig)
	})

	t.Run("negative sweep interval", func(t *testing.T) {
		t.Parallel()
		cfg := scheduler.DefaultConfig()
		cfg.SweepInterval = -time.Second
		assert.ErrorIs(t, cfg.Validate(), scheduler.ErrInvalidConfig)
	})

	t.Run("zero sweep interval disables the sweep", func(t *testing.T) {
		t.Parallel()
		cfg := scheduler.DefaultConfig()
		cfg.SweepInterval = 0
		require.NoError(t, cfg.Validate())
	})

	t.Run("zero retry interval", func(t *testing.T) {
		t.Parallel()
		cfg := scheduler.DefaultConfig()
		cfg.RetryInterval = 0
		assert.ErrorIs(t, cfg.Validate(), scheduler.ErrInvalidConfig)
	})

	t.Run("zero op timeout", func(t *testing.T) {
		t.Parallel()
		cfg := scheduler.DefaultConfig()
		cfg.OpTimeout = 0
		assert.ErrorIs(t, cfg.Validate(), scheduler.ErrInvalidConfig)
	})
}
