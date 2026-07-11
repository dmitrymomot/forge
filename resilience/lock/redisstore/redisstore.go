package redisstore

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// acquireScript claims the lock (SET NX PX) and returns a fresh monotonic fence
// (INCR on a companion key). If the caller already owns it, it refreshes the
// TTL and returns the current fence. Returns 0 when another owner holds it.
// KEYS[1]=lock KEYS[2]=fence  ARGV[1]=owner ARGV[2]=ttlMillis
var acquireScript = redis.NewScript(`
if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2]) then
  return redis.call('INCR', KEYS[2])
end
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return tonumber(redis.call('GET', KEYS[2]) or '0')
end
return 0`)

// refreshScript extends the TTL iff the caller owns the lock.
// KEYS[1]=lock ARGV[1]=owner ARGV[2]=ttlMillis
var refreshScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0`)

// releaseScript deletes the lock iff the caller owns it.
// KEYS[1]=lock ARGV[1]=owner
var releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0`)

type config struct{ prefix string }

// Option configures the Store.
type Option func(*config)

// WithPrefix namespaces all keys (e.g. "lock:").
func WithPrefix(p string) Option { return func(c *config) { c.prefix = p } }

// Store is a single-instance Redis implementation of lock.Store (SET NX PX +
// owner-checked Lua). It is NOT Redlock: multi-master safety is out of scope.
// The client's lifecycle is the caller's.
type Store struct {
	client redis.UniversalClient
	prefix string
}

// New builds a Redis lock Store.
func New(client redis.UniversalClient, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{client: client, prefix: c.prefix}
}

// lockKey and fenceKey hash-tag co-locate the lock and its fence counter on
// "{k}" so both land in the same Redis Cluster slot — acquireScript's 2-key
// EVAL would otherwise CROSSSLOT on a ClusterClient.
func (s *Store) lockKey(k string) string  { return s.prefix + "{" + k + "}" }
func (s *Store) fenceKey(k string) string { return s.prefix + "{" + k + "}:fence" }

// Acquire claims key for owner until now+ttl, returning a monotonic fencing
// token on success. ok is false if another live owner holds key.
func (s *Store) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (uint64, bool, error) {
	keys := []string{s.lockKey(key), s.fenceKey(key)}
	fence, err := acquireScript.Run(ctx, s.client, keys, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return 0, false, err
	}
	if fence <= 0 {
		return 0, false, nil
	}
	return uint64(fence), true, nil
}

// Refresh extends the lease iff owner still holds key; ok is false if lost.
func (s *Store) Refresh(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	n, err := refreshScript.Run(ctx, s.client, []string{s.lockKey(key)}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Release frees key iff held by owner (no-op otherwise).
func (s *Store) Release(ctx context.Context, key, owner string) error {
	return releaseScript.Run(ctx, s.client, []string{s.lockKey(key)}, owner).Err()
}
