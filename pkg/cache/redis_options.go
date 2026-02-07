package cache

import "time"

// RedisOption configures the Redis cache.
type RedisOption func(*redisOptions)

type redisOptions struct {
	prefix     string
	defaultTTL time.Duration
}

func defaultRedisOptions() *redisOptions {
	return &redisOptions{
		defaultTTL: time.Hour,
		prefix:     "",
	}
}

// WithRedisDefaultTTL sets the default expiration for cache entries when
// Set is called with a zero TTL.
// Default: 1 hour.
func WithRedisDefaultTTL(d time.Duration) RedisOption {
	return func(o *redisOptions) {
		o.defaultTTL = d
	}
}

// WithPrefix sets a key prefix for all cache operations.
// Keys are stored as "{prefix}:{key}". This is useful for namespacing
// when multiple caches share the same Redis instance.
func WithPrefix(prefix string) RedisOption {
	return func(o *redisOptions) {
		o.prefix = prefix
	}
}
