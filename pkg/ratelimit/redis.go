package ratelimit

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisConfig configures the Redis counter.
type RedisConfig struct {
	Prefix string `env:"PREFIX"`
}

// RedisCounter is a Redis-backed Counter implementation with automatic
// fallback to an in-memory counter on connection failure.
//
// Example:
//
//	client := redis.MustOpen(ctx, redisConfig)
//	counter := ratelimit.NewRedisCounter(client, ratelimit.RedisConfig{
//	    Prefix: "api",
//	})
type RedisCounter struct {
	client   redis.UniversalClient
	fallback *MemoryCounter
	prefix   string
	once     sync.Once
}

// NewRedisCounter creates a new Redis-backed counter.
// The client should be obtained from pkg/redis.Open or pkg/redis.MustOpen.
func NewRedisCounter(client redis.UniversalClient, cfg RedisConfig) *RedisCounter {
	return &RedisCounter{
		client: client,
		prefix: cfg.Prefix,
	}
}

// Increment atomically adds n to the count for the given key and window in Redis.
// Falls back to in-memory counter on Redis failure.
func (r *RedisCounter) Increment(ctx context.Context, key string, window time.Time, ttl time.Duration, n int64) (int64, error) {
	rkey := r.redisKey(key, window)

	pipe := r.client.Pipeline()
	incrCmd := pipe.IncrBy(ctx, rkey, n)
	pipe.Expire(ctx, rkey, ttl)
	_, err := pipe.Exec(ctx)
	if err != nil {
		slog.Warn("ratelimit: redis increment failed, using fallback", "error", err, "key", key)
		return r.getFallback().Increment(ctx, key, window, ttl, n)
	}

	return incrCmd.Val(), nil
}

// Get returns the current count for the given key and window from Redis.
// Falls back to in-memory counter on Redis failure.
func (r *RedisCounter) Get(ctx context.Context, key string, window time.Time) (int64, error) {
	rkey := r.redisKey(key, window)

	val, err := r.client.Get(ctx, rkey).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		slog.Warn("ratelimit: redis get failed, using fallback", "error", err, "key", key)
		return r.getFallback().Get(ctx, key, window)
	}

	return val, nil
}

// Close releases fallback resources if initialized.
// The Redis client lifecycle is managed separately by the caller.
func (r *RedisCounter) Close() error {
	if r.fallback != nil {
		return r.fallback.Close()
	}
	return nil
}

// redisKey formats the full Redis key with prefix and window timestamp.
// When no prefix is configured, "rl" is used as a default namespace
// to avoid collisions with other application keys.
func (r *RedisCounter) redisKey(key string, window time.Time) string {
	ts := strconv.FormatInt(window.Unix(), 10)
	if r.prefix == "" {
		return "rl:" + key + ":" + ts
	}
	return r.prefix + ":" + key + ":" + ts
}

// getFallback lazily initializes and returns the in-memory fallback counter.
func (r *RedisCounter) getFallback() *MemoryCounter {
	r.once.Do(func() {
		r.fallback = NewMemoryCounter(MemoryConfig{})
	})
	return r.fallback
}

var _ Counter = (*RedisCounter)(nil)
