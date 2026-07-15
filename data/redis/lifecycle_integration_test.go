//go:build integration

package redis_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/data/redis"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

func TestHealthcheck_OK(t *testing.T) {
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{redistest.Addr(t)}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	probe := forgeredis.Healthcheck(c)
	require.NotNil(t, probe)
	require.NoError(t, probe(t.Context()), "a live server must pass the healthcheck")
}
