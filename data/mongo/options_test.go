package mongo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgemongo "github.com/dmitrymomot/forge/data/mongo"
)

func TestOpen_NilOptionRejected(t *testing.T) {
	// Nil function/pointer arguments accumulate an ErrInvalidConfig and are surfaced
	// by Open before any connection attempt. A valid URI is supplied so the failure
	// is unambiguously the option's rejection, not a missing-URI validation error.
	cfg := forgemongo.DefaultConfig()
	cfg.URI = "mongodb://127.0.0.1:27017"
	cfg.Database = "forge_test"

	opts := map[string]forgemongo.Option{
		"clientoptions": forgemongo.WithClientOptions(nil),
		"logger":        forgemongo.WithLogger(nil),
	}
	for name, opt := range opts {
		t.Run(name, func(t *testing.T) {
			c, err := forgemongo.Open(t.Context(), forgemongo.WithConfig(cfg), opt)
			require.Error(t, err)
			assert.Nil(t, c)
			assert.ErrorIs(t, err, forgemongo.ErrInvalidConfig)
		})
	}
}

func TestOpen_MissingURIFailsValidate(t *testing.T) {
	// Omitting WithConfig runs on pure DefaultConfig(), whose empty URI fails Validate.
	c, err := forgemongo.Open(t.Context())
	require.Error(t, err)
	assert.Nil(t, c)
	assert.ErrorIs(t, err, forgemongo.ErrInvalidConfig)
}
