// Package singleflight coalesces concurrent calls for the same key into a
// single execution whose result is shared among all callers.
//
//	var g singleflight.Group[User]
//	u, shared, err := g.Do(ctx, id, func(ctx context.Context) (User, error) {
//	    return repo.Load(ctx, id)
//	})
//
// If fn panics, waiters receive an error and the leader's caller re-panics.
package singleflight
