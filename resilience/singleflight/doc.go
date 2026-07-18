// Package singleflight coalesces concurrent calls for the same key into a
// single execution whose result is shared among all callers.
//
// If fn panics, waiters receive an error and the leader's caller re-panics.
//
// [Group.Do] blocks every caller until the shared execution finishes.
// [Group.DoDetached] instead runs fn in its own goroutine and bounds each
// caller's wait by its own context — for hot paths where a stuck fn must not
// pin requests past their deadlines while the shared work still completes
// once.
//
// # Usage
//
//	var g singleflight.Group[User]
//	u, shared, err := g.Do(ctx, id, func(ctx context.Context) (User, error) {
//		return repo.Load(ctx, id)
//	})
package singleflight
