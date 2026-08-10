// Package cache is a typed cache over a pluggable byte-level Store. Build a
// Store (the bundled in-memory one, or a backend of your own — Redis is a
// natural fit) and wrap it with a typed facade. The facade never owns the
// Store's lifecycle.
//
// # Usage
//
//	store := cache.NewMemoryStore(cache.WithMaxEntries(10_000))
//	defer store.Close()
//
//	users := cache.New[User](store, cache.WithPrefix("users:"), cache.WithDefaultTTL(30*time.Minute))
//	u, err := users.GetOrSet(ctx, id, func(ctx context.Context) (User, time.Duration, error) {
//	    u, err := repo.Load(ctx, id)
//	    return u, 5 * time.Minute, err
//	})
//
// Claim a one-shot key (idempotency / session start):
//
//	err := store.Set(ctx, "idem:"+key, marker, cache.WithSetNonExist(), cache.WithTTL(24*time.Hour))
//	switch {
//	case errors.Is(err, cache.ErrExists):
//	    // lost the claim — replay or reject
//	case err != nil:
//	    // real failure
//	default:
//	    // won the claim — do the work exactly once
//	}
package cache
