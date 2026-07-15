package redisqueue_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	redisqueue "github.com/dmitrymomot/forge/async/queue/redis"
)

func TestRedisQueue_ValidatesConstruction(t *testing.T) {
	t.Parallel()
	_, err := redisqueue.New(nil)
	require.Error(t, err)
}
