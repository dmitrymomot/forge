package redis_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	goredis "github.com/redis/go-redis/v9"

	forgeredis "github.com/dmitrymomot/forge/data/redis"
)

func TestIsNil(t *testing.T) {
	// goredis.Nil is the cache-miss sentinel; IsNil must recognize it, including when
	// it has been wrapped, and reject unrelated errors.
	assert.True(t, forgeredis.IsNil(goredis.Nil))
	assert.True(t, forgeredis.IsNil(fmt.Errorf("get failed: %w", goredis.Nil)))
	assert.False(t, forgeredis.IsNil(nil))
	assert.False(t, forgeredis.IsNil(errors.New("some other error")))
}
