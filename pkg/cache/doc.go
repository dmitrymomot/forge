// Package cache provides a generic Cache interface with in-memory and Redis implementations.
//
// Both implementations share the same [Cache] interface, making it easy to swap
// backends or use in-memory caching for development and Redis for production.
//
// # Interface
//
// The [Cache] interface is generic over value type V:
//
//   - Get(ctx, key) (V, error) — retrieve a value
//   - Set(ctx, key, value, ttl) error — store a value with TTL
//   - Delete(ctx, key) error — remove a key
//   - Has(ctx, key) (bool, error) — check existence
//   - Clear(ctx) error — remove all entries
//   - Close() error — release resources
//
// TTL semantics for Set:
//   - Positive duration: item expires after this duration
//   - Zero: use the cache's configured default TTL (1 hour by default)
//   - Negative: item never expires
//
// # In-Memory Cache
//
// Use [NewMemory] for single-process applications or testing.
// It uses a hash map for O(1) lookups and a doubly-linked list for O(1)
// LRU eviction, with TTL-based expiration via a background janitor goroutine:
//
//	c := cache.NewMemory[string](
//	    cache.WithDefaultTTL(5 * time.Minute),
//	    cache.WithCleanupInterval(30 * time.Second),
//	    cache.WithMaxEntries(10000),
//	)
//	defer c.Close()
//
//	c.Set(ctx, "greeting", "hello", 0)   // uses default TTL
//	val, err := c.Get(ctx, "greeting")   // val = "hello"
//
// # Eviction Callbacks
//
// The in-memory cache supports eviction callbacks for resource cleanup:
//
//	c := cache.NewMemory[*Connection](
//	    cache.WithMaxEntries(100),
//	)
//	c.SetEvictCallback(func(key string, conn *Connection) {
//	    conn.Close()
//	})
//
// The callback is triggered on LRU eviction, TTL expiration cleanup,
// manual deletion, and clearing.
//
// # Redis Cache
//
// Use [NewRedis] for distributed caching with a Redis backend.
// Requires a [github.com/redis/go-redis/v9.UniversalClient]
// from [github.com/dmitrymomot/forge/pkg/redis]:
//
//	client := redis.MustOpen(ctx, os.Getenv("REDIS_URL"))
//	c := cache.NewRedis[User](client, nil,
//	    cache.WithPrefix("users"),
//	    cache.WithRedisDefaultTTL(30 * time.Minute),
//	)
//
//	c.Set(ctx, "user:123", user, time.Hour)
//	val, err := c.Get(ctx, "user:123")
//
// Pass a custom [Marshaler] as the second argument to [NewRedis] to use
// a different serialization format (msgpack, protobuf, etc.).
// If nil, JSON is used.
//
// # Cache Stampede Prevention
//
// Use the standalone [GetOrSet] function to prevent cache stampedes.
// It uses singleflight to ensure only one goroutine computes a missing value:
//
//	val, err := cache.GetOrSet(ctx, c, "user:123", func(ctx context.Context) (User, time.Duration, error) {
//	    user, err := repo.FindUser(ctx, "123")
//	    return user, 5 * time.Minute, err
//	})
//
// # Error Handling
//
// The package defines sentinel errors:
//
//   - [ErrNotFound] — key does not exist or has expired
//   - [ErrClosed] — operation on a closed cache
//   - [ErrMarshal] — value serialization failed
//   - [ErrUnmarshal] — value deserialization failed
//
// Use [errors.Is] to check:
//
//	val, err := c.Get(ctx, "key")
//	if errors.Is(err, cache.ErrNotFound) {
//	    // handle miss
//	}
package cache
