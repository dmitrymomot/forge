package opensearch_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/data/opensearch"
)

func TestDefaultConfig(t *testing.T) {
	cfg := forgeos.DefaultConfig()
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 10*time.Second, cfg.RequestTimeout)
	assert.Equal(t, 3, cfg.RetryAttempts)
	assert.Equal(t, time.Second, cfg.RetryInterval)
	assert.False(t, cfg.InsecureSkipVerify)
	assert.Empty(t, cfg.Addresses)
	assert.Empty(t, cfg.Username)
	assert.Empty(t, cfg.Password)
	// DefaultConfig alone is not valid: Addresses is required.
	require.Error(t, cfg.Validate())

	// A minimally valid config validates clean.
	cfg.Addresses = []string{"http://localhost:9200"}
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	base := func() forgeos.Config {
		c := forgeos.DefaultConfig()
		c.Addresses = []string{"http://localhost:9200"}
		return c
	}

	bad := map[string]forgeos.Config{
		"empty addresses": func() forgeos.Config { c := base(); c.Addresses = nil; return c }(),
		"blank address":   func() forgeos.Config { c := base(); c.Addresses = []string{"  "}; return c }(),
		"neg max retries": func() forgeos.Config { c := base(); c.MaxRetries = -1; return c }(),
		"neg req timeout": func() forgeos.Config { c := base(); c.RequestTimeout = -1; return c }(),
		"neg retry att":   func() forgeos.Config { c := base(); c.RetryAttempts = -1; return c }(),
		"neg retry intvl": func() forgeos.Config { c := base(); c.RetryInterval = -1; return c }(),
	}
	for name, cfg := range bad {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, forgeos.ErrInvalidConfig)
		})
	}

	require.NoError(t, base().Validate())
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"Addresses":          "OPENSEARCH_ADDRESSES",
		"Username":           "OPENSEARCH_USERNAME",
		"Password":           "OPENSEARCH_PASSWORD",
		"InsecureSkipVerify": "OPENSEARCH_INSECURE_SKIP_VERIFY",
		"MaxRetries":         "OPENSEARCH_MAX_RETRIES",
		"RequestTimeout":     "OPENSEARCH_REQUEST_TIMEOUT",
		"RetryAttempts":      "OPENSEARCH_RETRY_ATTEMPTS",
		"RetryInterval":      "OPENSEARCH_RETRY_INTERVAL",
	}
	typ := reflect.TypeFor[forgeos.Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
	// Addresses is []string; caarlos0/env parses comma-separated values into it,
	// but the tag itself is still the plain name ADDRESSES.
	f, _ := typ.FieldByName("Addresses")
	assert.Equal(t, reflect.TypeFor[[]string](), f.Type)
}
