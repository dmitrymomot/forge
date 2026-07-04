// Package redis provides a Redis-backed cache.Store.
//
// # Usage
//
//	client, err := forgeredis.Open(ctx, forgeredis.WithConfig(cfg)) // caller closes it
//	if err != nil {
//		// handle err
//	}
//	defer client.Close()
//
//	store := redis.NewStore(client)
//	sessions := cache.New[Session](store, cache.WithPrefix("sess:"))
//
// Store.Close is a no-op — close the underlying client yourself. Clear on a
// typed cache issues SCAN + DEL over the cache's prefix (never FLUSHDB).
package redis
