// Package redisqueue is the Redis queue.Broker: one stream + consumer group
// per queue for claim-with-lease (XAUTOCLAIM redelivers entries idle longer
// than the lease), a sorted set staging delayed and retried jobs (promoted
// atomically by a Lua script during Claim), and a hash holding dead-letter
// payloads. No transactional enqueue — use async/queue/postgres,
// async/queue/mongo, or, once it lands, async/outbox.
//
// Claim ownership is tracked in-process, not in redis: Broker keeps a
// job-id-keyed map of live claims in memory, so a fencing token from Claim is
// only valid against the same Broker instance — handing it to another
// instance (or a second Broker over the same redis) always returns
// ErrLeaseLost, even though the token itself is not expired. pgqueue and
// queue.MemoryBroker track ownership in shared storage instead and don't have
// this restriction; see the queue.Broker doc for the portability contract.
//
// Requires Redis >= 8. This is a tested-floor statement, not a feature
// dependency: every command this driver uses has existed since Redis 7.0.
//
// # Key layout
//
// All keys live under a configurable prefix (default "queue:"). Per queue q:
// "<prefix>q" is the stream, "<prefix>q:delayed" the staging sorted set,
// "<prefix>q:data" the payload hash backing the staging set, "<prefix>q:dead"
// the dead-letter payload hash, "<prefix>q:dead:idx" a sorted set keyed by
// kill time (so ListDead is a plain range read, never O(DLQ)), and
// "<prefix>q:poison" a list of undecodable stream entries. "<prefix>queues"
// and "<prefix>index" are prefix-wide registries shared across queues.
//
// An undecodable stream entry (a foreign XADD, or a future wire version) is
// parked to its queue's poison list instead of failing Claim forever —
// nothing decodes or retries poisoned entries automatically, so check the
// poison list in ops runbooks.
//
// Broker implements queue.Maintainer: Maintain deletes consumers that have no
// pending entries and have been idle past WithConsumerIdleCutoff (default
// 1h), and prunes queues left fully empty (stream, delayed set, dead store,
// and poison list all empty) from the registry.
package redisqueue
