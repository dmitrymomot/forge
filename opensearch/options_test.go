package opensearch_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	opts := map[string]forgeos.Option{
		"client config": forgeos.WithClientConfig(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			cfg := forgeos.DefaultConfig()
			cfg.Addresses = []string{"http://127.0.0.1:9200"}
			// A valid Config is supplied so the failure is unambiguously the nil
			// option's rejection (ErrInvalidConfig), not a Validate failure.
			_, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg), opt)
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeos.ErrInvalidConfig)
		})
	}
}

func TestOpen_InvalidConfigRejected(t *testing.T) {
	// No Addresses -> Validate fails before any connection attempt.
	_, err := forgeos.Open(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrInvalidConfig)
}

func TestOpen_NilLoggerAllowed(t *testing.T) {
	// A nil logger is not a validation error; it is replaced by a discard logger.
	// Point at an unreachable address with a 1-attempt budget so Open returns fast
	// with ErrConnect (proving WithLogger(nil) was accepted, not rejected).
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{"http://127.0.0.1:1"}
	cfg.RetryAttempts = 1
	cfg.RetryInterval = time.Millisecond
	cfg.RequestTimeout = 50 * time.Millisecond
	_, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg), forgeos.WithLogger(nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, forgeos.ErrConnect)
	assert.NotErrorIs(t, err, forgeos.ErrInvalidConfig)
}
