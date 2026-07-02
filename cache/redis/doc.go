// Package redis provides a Redis-backed cache.Store.
//
//	client := redis.Open(ctx, redis.WithConfig(cfg)) // forge/redis; caller closes it
//	defer client.Close()
//
//	store := cacheredis.NewStore(client)
//	sessions := cache.New[Session](store, cache.WithPrefix("sess:"))
//
// Store.Close is a no-op — close the underlying client yourself. Clear on a
// typed cache issues SCAN + DEL over the cache's prefix (never FLUSHDB).
package redis
