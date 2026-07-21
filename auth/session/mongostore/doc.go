// Package mongostore is the MongoDB session.Store + session.UserIndex driver
// over the official v2 client — full multi-device management (device
// listings, per-device revocation, "log out everywhere", GDPR deletion) for
// Mongo-backed apps. Tokens are persisted only as SHA-256 digests, so a
// database leak exposes no usable session credentials.
//
// Construction performs no I/O; create the indexes once at boot:
//
//	store, err := mongostore.New(client.Database("app"))
//	if err != nil { ... }
//	if err := store.EnsureIndexes(ctx); err != nil { ... }
//	mgr, err := session.New[Data](store)
//
// Expired sessions are reaped by MongoDB's native TTL monitor (the
// expires_at TTL index, roughly once a minute) — no sweep job to schedule;
// the Manager refuses expired records regardless, so the reaping lag is
// invisible to callers. Reads are pinned to the primary: a session store
// must read its own acknowledged writes, and a secondary-lagging read would
// miss a just-saved session or resurrect a just-revoked one.
//
// BSON datetimes carry millisecond precision, so stored timestamps are
// truncated to the millisecond on round-trip.
package mongostore
