package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// RedisConfig configures the Redis cache.
type RedisConfig struct {
	Prefix     string        `env:"PREFIX"`
	DefaultTTL time.Duration `env:"DEFAULT_TTL" envDefault:"5m"`
}

// Redis is a cache backed by Redis.
// It serializes values using the configured Marshaler (default: JSON).
type Redis[V any] struct {
	client     redis.UniversalClient
	marshaler  Marshaler[V]
	sf         singleflight.Group
	prefix     string
	defaultTTL time.Duration
}

// NewRedis creates a new Redis-backed cache.
// The client should be obtained from pkg/redis.Open or pkg/redis.MustOpen.
//
// An optional Marshaler can be provided to customize serialization.
// If nil, JSON serialization is used.
//
// Example:
//
//	client := redis.MustOpen(ctx, redis.Config{URL: os.Getenv("REDIS_URL")})
//	c := cache.NewRedis[User](client, nil, cache.RedisConfig{
//	    Prefix:     "users",
//	    DefaultTTL: 30 * time.Minute,
//	})
func NewRedis[V any](client redis.UniversalClient, m Marshaler[V], cfg RedisConfig) *Redis[V] {
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}

	if m == nil {
		m = jsonMarshaler[V]{}
	}

	return &Redis[V]{
		client:     client,
		marshaler:  m,
		prefix:     cfg.Prefix,
		defaultTTL: cfg.DefaultTTL,
	}
}

// Get retrieves a value by key from Redis.
// Returns ErrNotFound if the key does not exist.
func (r *Redis[V]) Get(ctx context.Context, key string) (V, error) {
	var zero V

	data, err := r.client.Get(ctx, r.prefixedKey(key)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return zero, ErrNotFound
		}
		return zero, err
	}

	v, err := r.marshaler.Unmarshal(data)
	if err != nil {
		return zero, err
	}

	return v, nil
}

// Set stores a value in Redis with the given TTL.
// TTL semantics: positive = expires after duration, zero = use default TTL,
// negative = no expiration (persists until manually deleted or Redis evicts it).
func (r *Redis[V]) Set(ctx context.Context, key string, value V, ttl time.Duration) error {
	data, err := r.marshaler.Marshal(value)
	if err != nil {
		return err
	}

	// Resolve TTL.
	if ttl == 0 {
		ttl = r.defaultTTL
	}

	// Redis interprets 0 as no expiration.
	// For negative TTL (our "never expires" semantic), pass 0 to Redis.
	redisTTL := max(ttl, 0)

	return r.client.Set(ctx, r.prefixedKey(key), data, redisTTL).Err()
}

// Delete removes a key from Redis.
func (r *Redis[V]) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, r.prefixedKey(key)).Err()
}

// Has checks whether a key exists in Redis.
func (r *Redis[V]) Has(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, r.prefixedKey(key)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Clear removes all cache entries that match the configured prefix using SCAN
// (non-blocking, safe for production).
//
// If no prefix is configured, Clear returns ErrNoPrefix instead of wiping the
// whole Redis database: an unprefixed cache shares the database with every
// other key, so an accidental FLUSHDB here would destroy unrelated data. To
// deliberately wipe the entire database, call FlushDB explicitly.
func (r *Redis[V]) Clear(ctx context.Context) error {
	if r.prefix == "" {
		return ErrNoPrefix
	}
	return r.clearByPrefix(ctx)
}

// FlushDB removes every key in the underlying Redis database, not just keys
// belonging to this cache's prefix. This is a destructive, explicit-opt-in
// operation that bypasses the prefix isolation Clear enforces; use it only when
// you intend to wipe the entire database (e.g., in tests).
func (r *Redis[V]) FlushDB(ctx context.Context) error {
	return r.client.FlushDB(ctx).Err()
}

// Close is a no-op for Redis. The Redis client lifecycle is managed
// separately by the caller (via pkg/redis.Shutdown).
func (r *Redis[V]) Close() error {
	return nil
}

// prefixedKey returns the full Redis key with prefix.
func (r *Redis[V]) prefixedKey(key string) string {
	if r.prefix == "" {
		return key
	}
	return r.prefix + ":" + key
}

// clearByPrefix removes all keys matching the configured prefix using SCAN.
// This is safe for production use as SCAN does not block the server.
func (r *Redis[V]) clearByPrefix(ctx context.Context) error {
	pattern := r.prefix + ":*"
	var cursor uint64

	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}

		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

func (r *Redis[V]) sfDo(key string, fn func() (any, error)) (any, error) {
	v, err, _ := r.sf.Do(key, fn)
	return v, err
}

var _ Cache[any] = (*Redis[any])(nil)
