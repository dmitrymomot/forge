package redis_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/data/redis"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	opts := map[string]forgeredis.Option{
		"universaloptions": forgeredis.WithUniversalOptions(nil),
		"logger":           forgeredis.WithLogger(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			// A valid Addresses is supplied so the only fault is the nil option:
			// the failure must be ErrInvalidConfig, surfaced before any dial.
			c, err := forgeredis.Open(t.Context(),
				forgeredis.WithConfig(validConfig()),
				opt,
			)
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeredis.ErrInvalidConfig)
			assert.Nil(t, c, "no client is returned on a config/option error")
		})
	}
}

func TestOpen_InvalidConfigRejected(t *testing.T) {
	// Pure DefaultConfig has no Addresses -> Validate fails -> ErrInvalidConfig,
	// with no network attempt.
	c, err := forgeredis.Open(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeredis.ErrInvalidConfig)
	assert.Nil(t, c)
}
