package redisstore

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrScript increments the key and sets the TTL only on creation (when the new
// value equals the delta), so a window's expiry is fixed at its first hit.
var incrScript = redis.NewScript(`
local v = redis.call('INCRBY', KEYS[1], ARGV[1])
if v == tonumber(ARGV[1]) then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return v`)

type config struct {
	prefix string
}

// Option configures the Store.
type Option func(*config)

// WithPrefix namespaces all keys (e.g. "rl:").
func WithPrefix(p string) Option { return func(c *config) { c.prefix = p } }

// Store implements ratelimit.Store over a go-redis client. The client's
// lifecycle is the caller's; Close is a no-op.
type Store struct {
	client redis.UniversalClient
	prefix string
}

// New builds a Redis-backed counter Store.
func New(client redis.UniversalClient, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{client: client, prefix: c.prefix}
}

func (s *Store) key(k string) string { return s.prefix + k }

// Incr atomically adds delta and sets ttl only when the key is newly created.
func (s *Store) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	return incrScript.Run(ctx, s.client, []string{s.key(key)}, delta, ttl.Milliseconds()).Int64()
}

// Get returns the counter, or 0 when absent.
func (s *Store) Get(ctx context.Context, key string) (int64, error) {
	n, err := s.client.Get(ctx, s.key(key)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

// Reset deletes the counter.
func (s *Store) Reset(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.key(key)).Err()
}

// Close is a no-op; the client is owned by the caller.
func (s *Store) Close() error { return nil }
