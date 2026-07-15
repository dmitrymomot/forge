//go:build integration

package redis_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeredis "github.com/dmitrymomot/forge/data/redis"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

type jsonValue struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGetSetJSON_RoundTrip(t *testing.T) {
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{redistest.Addr(t)}

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
	cfg := forgeredis.DefaultConfig()
	cfg.Addresses = []string{redistest.Addr(t)}

	c, err := forgeredis.Open(t.Context(), forgeredis.WithConfig(cfg))
	require.NoError(t, err)
	defer forgeredis.Close(c, slogDiscard())

	missingKey := fmt.Sprintf("forge:test:missing:%d", time.Now().UnixNano())
	got, err := forgeredis.GetJSON[jsonValue](t.Context(), c, missingKey)
	require.Error(t, err)
	assert.True(t, forgeredis.IsNil(err), "a missing key must surface as a goredis.Nil miss")
	assert.Equal(t, jsonValue{}, got, "a miss returns the zero value of T")
}
