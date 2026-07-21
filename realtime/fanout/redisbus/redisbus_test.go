package redisbus_test

import (
	"context"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/supervisor"
	"github.com/dmitrymomot/forge/realtime/fanout"
	"github.com/dmitrymomot/forge/realtime/fanout/redisbus"
)

var (
	_ fanout.Bus         = (*redisbus.Bus)(nil)
	_ supervisor.Service = (*redisbus.Bus)(nil)
)

// testClient builds a client without connecting: go-redis dials lazily.
func testClient(t *testing.T) goredis.UniversalClient {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "localhost:1"})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		_, err := redisbus.New(nil)
		assert.Error(t, err)
	})

	t.Run("empty channel", func(t *testing.T) {
		t.Parallel()
		_, err := redisbus.New(testClient(t), redisbus.WithChannel(""))
		assert.Error(t, err)
	})

	t.Run("name carries the channel", func(t *testing.T) {
		t.Parallel()
		bus, err := redisbus.New(testClient(t), redisbus.WithChannel("app:events"))
		require.NoError(t, err)
		assert.Equal(t, "fanout.redisbus:app:events", bus.Name())
	})
}

func TestPublishValidation(t *testing.T) {
	t.Parallel()

	bus, err := redisbus.New(testClient(t))
	require.NoError(t, err)
	err = bus.Publish(context.Background(), "bad\x00topic", []byte("x"))
	assert.ErrorIs(t, err, redisbus.ErrInvalidTopic)
}
