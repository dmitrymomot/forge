// Package pgqueue is the Postgres queue.Broker: claim-with-lease via
// FOR UPDATE SKIP LOCKED over a single table, crash recovery for free
// (an expired claimed_until makes the row claimable again — no reaper),
// and transactional enqueue via the queue.TxPusher capability (pass a
// pgx.Tx to queue.PushTx). Apply Migrations with data/migration before use.
// There is no LISTEN/NOTIFY: the engine's poll ticker drives claiming.
package pgqueue
