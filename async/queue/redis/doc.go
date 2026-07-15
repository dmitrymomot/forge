// Package redisqueue is the Redis queue.Broker: one stream + consumer group
// per queue for claim-with-lease (XAUTOCLAIM redelivers entries idle longer
// than the lease), a sorted-set staging area for delayed and retried jobs
// (promoted atomically by a Lua script during Claim), and hashes for the
// dead-letter set. No transactional enqueue — use async/queue/postgres or,
// once it lands, async/outbox. All keys live under a configurable prefix.
package redisqueue
