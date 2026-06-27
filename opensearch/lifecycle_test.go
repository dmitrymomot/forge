package opensearch_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/opensearch"
)

func TestClose_NilTolerated(t *testing.T) {
	// Close must tolerate a nil client and a nil logger without panicking; it never
	// touches the network (the HTTP client owns no persistent sockets to release).
	assert.NotPanics(t, func() { forgeos.Close(nil, nil) })

	// When a live server is available, Close on a real client is also a no-op.
	addr := os.Getenv("FORGE_TEST_OPENSEARCH_ADDR")
	if addr == "" {
		return
	}
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{addr}
	cfg.RetryAttempts = 5
	cfg.RetryInterval = 500 * time.Millisecond
	cfg.RequestTimeout = 5 * time.Second
	client, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.NoError(t, err)
	assert.NotPanics(t, func() { forgeos.Close(client, nil) })
}

func TestHealthcheck_Integration(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_OPENSEARCH_ADDR")
	if addr == "" {
		t.Skip("set FORGE_TEST_OPENSEARCH_ADDR to run the opensearch integration test")
	}
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{addr}
	cfg.RetryAttempts = 5
	cfg.RetryInterval = 500 * time.Millisecond
	cfg.RequestTimeout = 5 * time.Second

	client, err := forgeos.Open(t.Context(), forgeos.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeos.Close(client, nil)

	check := forgeos.Healthcheck(client)
	require.NotNil(t, check)
	require.NoError(t, check(t.Context()))
}
