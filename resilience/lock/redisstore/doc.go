// Package redisstore implements lock.Store over a go-redis client, using
// SET NX PX for mutual exclusion, owner-checked Lua for refresh/release, and
// a companion INCR key for monotonic fencing tokens.
//
// It is NOT Redlock — multi-master/quorum locking is out of scope. For a
// single Redis instance it is correct and fast.
//
// The lock and fence keys are hash-tag co-located (both hash on "{key}"), so
// a single lock's keys always land in the same Redis Cluster slot.
//
// The fence key intentionally never expires — it must survive across
// acquisitions to keep fencing tokens monotonic — while the lock key
// self-expires via PX. A namespace with many short-lived, uniquely-named
// locks therefore accumulates orphaned fence counters over time. If key
// cardinality is high and lock names are not reused, prune stale fence keys
// out-of-band (e.g. by prefix scan and age) rather than relying on Redis
// expiry.
//
// # Usage
//
//	l := lock.New(redisstore.New(client, redisstore.WithPrefix("lock:")))
package redisstore
