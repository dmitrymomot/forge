// Package pgqueue is the Postgres queue.Broker: SKIP LOCKED claiming with
// fencing tokens over a hot jobs table, a separate cold dead-letter table,
// bounded stats, and transactional enqueue via the queue.TxPusher capability
// (pass a pgx.Tx to queue.PushTx). Apply Migrations with data/migration
// before use. There is no LISTEN/NOTIFY: the engine's poll ticker drives
// claiming.
//
// Requires PostgreSQL >= 18 (distinct-queue enumeration in Stats leans on
// B-tree skip scan; earlier servers work but Stats may plan a seq scan).
//
// # Two-table layout
//
// Live jobs live in the hot table (queue_jobs by default); Kill moves a job
// to the cold dead-letter table in the same statement that removes it from
// the hot one, so the hot table never accumulates dead-letter rows. WithTable
// overrides the hot table name; the dead table name is always derived as
// "<table>_dead" — a custom name requires a caller-managed schema of the same
// shape as the shipped migration.
//
// Stats counts are exact up to 10,000 rows per queue; beyond that a query
// stops at the cap and QueueStats reports the cap with its Capped flag set,
// keeping Stats O(cap) instead of O(table).
//
// Push and PushTx switch from a single unnest-array INSERT to pgx.CopyFrom
// once a batch reaches 2,000 jobs — a measured ~40% win at 10k rows — so
// large batches get the cheaper strategy automatically.
package pgqueue
