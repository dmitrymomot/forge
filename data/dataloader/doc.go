// Package dataloader collapses N+1 lookups with a generic per-request
// batch-and-cache loader. Keys requested concurrently or in bulk within a
// short window are deduplicated and handed to one caller-owned BatchFunc
// call; every resolved key — value, not-found, or error — is memoized for
// the loader's lifetime. Pure generics, no storage imports: the BatchFunc
// owns the actual lookup (SQL IN-list, cache MGET, RPC).
//
// A Loader is a per-request object. Construct one per request so memoized
// results never outlive the request or leak across tenants; in multi-tenant
// apps the BatchFunc closure carries the tenant scope (it receives the
// context values of the caller that opened each batch, detached from that
// caller's cancellation). Because the batch context carries no deadline, the
// BatchFunc must bound its own work — set a query timeout inside it.
//
// Batching has two triggers: the wait window (default 1ms) and the batch
// size cap (default 100), whichever comes first. Sequential per-item Loads
// each pay the window; collapse loops with LoadMany, which schedules all its
// keys into shared batches before waiting.
//
// # Usage
//
//	loader := dataloader.New(func(ctx context.Context, ids []string) (map[string]User, error) {
//		return repo.UsersByIDs(ctx, ids) // one SELECT ... WHERE id = ANY($1)
//	})
//
//	// Concurrent resolvers coalesce into one query per window.
//	u, err := loader.Load(ctx, comment.AuthorID)
//
//	// A loop's lookups become one query.
//	authors, err := loader.LoadMany(ctx, authorIDs)
package dataloader
