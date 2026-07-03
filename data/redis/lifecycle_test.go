package redis_test

import (
	"os"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/data/redis"
)

func TestClose_NilTolerant(t *testing.T) {
	// Close must not panic on a nil client and/or a nil logger; it is a defer helper
	// in main and has to be defensive on every shutdown path.
	assert.NotPanics(t, func() { forgeredis.Close(nil, nil) })
	assert.NotPanics(t, func() { forgeredis.Close(nil, slogDiscard()) })

	// A non-nil client with a nil logger must close without panicking. go-redis
	// constructs lazily, so no server is needed to build and immediately close it.
	client := goredis.NewUniversalClient(&goredis.UniversalOptions{Addrs: []string{"127.0.0.1:1"}})
	assert.NotPanics(t, func() { forgeredis.Close(client, nil) })
}

func TestHealthcheck_OK(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port) to run the redis integration test")
	}
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{addr}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	probe := forgeredis.Healthcheck(c)
	require.NotNil(t, probe)
	require.NoError(t, probe(t.Context()), "a live server must pass the healthcheck")
}
