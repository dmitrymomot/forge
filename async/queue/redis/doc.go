// Package redisqueue is the Redis queue.Broker: one stream + consumer group
// per queue for claim-with-lease (XAUTOCLAIM redelivers entries idle longer
// than the lease), a sorted-set staging area for delayed and retried jobs
// (promoted atomically by a Lua script during Claim), and hashes for the
// dead-letter set. No transactional enqueue — use async/queue/postgres or,
// once it lands, async/outbox. All keys live under a configurable prefix.
//
// An undecodable stream entry (a foreign XADD, or a future wire version) is
// parked to a per-queue poison list instead of failing Claim forever. Broker
// implements queue.Maintainer: Maintain deletes zero-pending consumers idle
// past WithConsumerIdleCutoff and prunes queues left fully empty (stream,
// delayed set, dead store, and poison list all empty) from the registry.
package redisqueue
