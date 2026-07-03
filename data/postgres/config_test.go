package postgres_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/data/postgres"
)

func TestDefaultConfig(t *testing.T) {
	cfg := postgres.DefaultConfig()
	assert.Equal(t, int32(10), cfg.MaxConns)
	assert.Equal(t, int32(2), cfg.MinConns)
	assert.Equal(t, 30*time.Minute, cfg.MaxConnLifetime)
	assert.Equal(t, 10*time.Minute, cfg.MaxConnIdleTime)
	assert.Equal(t, time.Minute, cfg.HealthCheckPeriod)
	assert.Equal(t, 5*time.Second, cfg.ConnectTimeout)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)
	assert.Empty(t, cfg.URL, "URL defaults empty and is required at Open time")

	// DefaultConfig alone fails Validate because URL is required.
	require.ErrorIs(t, cfg.Validate(), postgres.ErrInvalidConfig)

	// A default config with a URL filled in is valid.
	cfg.URL = "postgres://u:p@localhost:5432/db"
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	const url = "postgres://u:p@localhost:5432/db"
	tests := map[string]postgres.Config{
		"empty url":          {URL: ""},
		"neg max conns":      {URL: url, MaxConns: -1},
		"neg min conns":      {URL: url, MinConns: -1},
		"min gt max":         {URL: url, MinConns: 5, MaxConns: 2},
		"neg conn lifetime":  {URL: url, MaxConnLifetime: -1},
		"neg conn idle":      {URL: url, MaxConnIdleTime: -1},
		"neg health period":  {URL: url, HealthCheckPeriod: -1},
		"neg connect to":     {URL: url, ConnectTimeout: -1},
		"neg retry attempt":  {URL: url, RetryAttempts: -1},
		"neg retry interval": {URL: url, RetryInterval: -1},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, postgres.ErrInvalidConfig)
		})
	}

	// Zero MaxConns/MinConns are allowed (pgxpool fills its own defaults).
	require.NoError(t, postgres.Config{URL: url}.Validate())
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"URL":               "URL",
		"MinConns":          "MIN_CONNS",
		"MaxConns":          "MAX_CONNS",
		"MaxConnLifetime":   "MAX_CONN_LIFETIME",
		"MaxConnIdleTime":   "MAX_CONN_IDLE_TIME",
		"HealthCheckPeriod": "HEALTH_CHECK_PERIOD",
		"ConnectTimeout":    "CONNECT_TIMEOUT",
		"RetryAttempts":     "RETRY_ATTEMPTS",
		"RetryInterval":     "RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[postgres.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}
