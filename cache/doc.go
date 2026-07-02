// Package cache is a typed cache over a pluggable byte-level Store. Build a
// Store (in-memory or, via cache/redis, Redis) and wrap it with a typed facade.
// The facade never owns the Store's lifecycle.
//
//	store := cache.NewMemoryStore(cache.WithMaxEntries(10_000))
//	defer store.Close()
//
//	users := cache.New[User](store, cache.WithPrefix("users:"), cache.WithDefaultTTL(30*time.Minute))
//	u, err := users.GetOrSet(ctx, id, func(ctx context.Context) (User, time.Duration, error) {
//	    u, err := repo.Load(ctx, id)
//	    return u, 5 * time.Minute, err
//	})
package cache
