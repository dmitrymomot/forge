// Package mongoqueue is the MongoDB queue.Broker: guarded update-many
// claiming with fencing tokens over a single jobs collection, atomic
// single-document lifecycle transitions, bounded stats, and transactional
// enqueue via the queue.TxPusher capability (pass a *mongo.Session with a
// running transaction to queue.PushTx). Call EnsureIndexes once at boot,
// after data/mongo.Open. There is no change stream tailing: the engine's
// poll ticker drives claiming.
//
// Requires a replica set (or mongos) for multi-job Push and PushTx, which run
// inside a multi-document transaction to honor the all-or-nothing contract;
// every other operation — including single-job Push, the entire claim/ack
// lifecycle, and the dead-letter ops — is a single atomic command and works
// on a standalone server too. Tested against MongoDB 8.3; this is a
// tested-floor statement, not a feature dependency — every command this
// driver uses (partial indexes, findAndModify, multi-document transactions)
// has existed since MongoDB 4.2.
//
// # Single-collection layout
//
// Live and dead jobs share one collection (queue_jobs by default; WithCollection
// overrides) discriminated by a state field, so Kill and Requeue are single
// atomic updates — no cross-collection move that could lose a job between a
// delete and an insert. Partial indexes keep the hot claim index free of
// dead-letter entries and vice versa, so the shared collection costs the hot
// path nothing.
//
// Visibility is one always-set indexed watermark, visible_at: run_at on
// push, the lease deadline while claimed, the retry time after a nack. The
// claim scan is a single tight index range over {queue, visible_at, _id} —
// in-flight jobs sit beyond the watermark instead of being fetched and
// filtered out on every poll.
//
// A batch claim is a shortlist of candidate ids in visibility order, one
// guarded updateMany that stamps the lease, fencing token, and attempt
// increment only on documents that are still claimable, and one fetch of the
// won documents. Candidates lost to a concurrent claimer are refilled from
// the next shortlist (bounded rounds), so contention thins a batch rather
// than emptying it. A single-job claim is one atomic findAndModify.
//
// Due-ness, lease expiry, and purge cutoffs are compared against the worker
// process clock (like queue.MemoryBroker), not the server clock: filters stay
// plain and fully indexable. Clock skew between workers can only shift when a
// lease is considered expired; the fencing token keeps a stale worker's
// Extend/Ack/Nack/Kill from disturbing the new claim, so delivery stays
// at-least-once regardless of skew. The broker reads from the primary
// regardless of the client's read preference — a queue must read its own
// acknowledged writes.
//
// Stats counts are exact up to 10,000 documents per queue; beyond that the
// count stops at the cap and QueueStats reports the cap with its Capped flag
// set, keeping Stats O(cap) instead of O(collection).
//
// Push also composes with a caller-owned transaction without the TxPusher
// capability: run it with the session-bound context data/mongo.WithTransaction
// passes to its callback and the insert joins that transaction instead of
// starting its own. (Inside that callback the session for queue.PushTx is
// recoverable via mongo.SessionFromContext, but plain Push is the simpler
// path.) A session bound for causal consistency only — no running
// transaction — is not mistaken for one: Push falls back to its own
// transaction and PushTx rejects the session.
package mongoqueue
