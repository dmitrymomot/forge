//go:build integration

package opensearch_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	forgeos "github.com/dmitrymomot/forge/data/opensearch"
	"github.com/dmitrymomot/forge/testkit/opensearchtest"
)

func TestHealthcheck_Integration(t *testing.T) {
	cfg := forgeos.DefaultConfig()
	cfg.Addresses = []string{opensearchtest.Addr(t)}
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
