package redis_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	goredis "github.com/redis/go-redis/v9"

	forgeredis "github.com/dmitrymomot/forge/redis"
)

func TestIsNil(t *testing.T) {
	// goredis.Nil is the cache-miss sentinel; IsNil must recognize it, including when
	// it has been wrapped, and reject unrelated errors.
	assert.True(t, forgeredis.IsNil(goredis.Nil))
	assert.True(t, forgeredis.IsNil(fmt.Errorf("get failed: %w", goredis.Nil)))
	assert.False(t, forgeredis.IsNil(nil))
	assert.False(t, forgeredis.IsNil(errors.New("some other error")))
}

type jsonValue struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGetSetJSON_RoundTrip(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port) to run the redis integration test")
	}
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{addr}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	key := fmt.Sprintf("forge:test:json:%d", time.Now().UnixNano())
	t.Cleanup(func() { c.Del(context.Background(), key) })

	want := jsonValue{Name: "forge", Count: 7}
	require.NoError(t, forgeredis.SetJSON(t.Context(), c, key, want, time.Minute))

	got, err := forgeredis.GetJSON[jsonValue](t.Context(), c, key)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetJSON_Miss(t *testing.T) {
	addr := os.Getenv("FORGE_TEST_REDIS_URL")
	if addr == "" {
		t.Skip("set FORGE_TEST_REDIS_URL (host:port) to run the redis integration test")
	}
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{addr}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	missingKey := fmt.Sprintf("forge:test:missing:%d", time.Now().UnixNano())
	got, err := forgeredis.GetJSON[jsonValue](t.Context(), c, missingKey)
	require.Error(t, err)
	assert.True(t, forgeredis.IsNil(err), "a missing key must surface as a goredis.Nil miss")
	assert.Equal(t, jsonValue{}, got, "a miss returns the zero value of T")
}
