// Package outbox is the transactional outbox: job intent rows commit inside
// the business database transaction, and a relay service forwards committed
// rows into any queue.Broker — the bridge from a business-database
// transaction to broker delivery. Brokers whose store has real multi-document
// transactions implement queue.TxPusher natively and do
// not need this package unless the business data lives in a different
// database than the queue.
//
// Wrap decorates a broker with queue.TxPusher backed by the outbox Store, so
// the existing producer APIs work unchanged: queue.PushTx and
// eventbus.PublishTx write intent rows through the wrapped broker, and
// non-transactional pushes keep going straight to the wrapped broker.
//
// # Usage
//
//	store := newOutboxStore(pool)            // your outbox.Store over the business DB
//	broker := outbox.Wrap(store, baseBroker)  // adds PushTx to a broker without it
//	client := queue.NewClient(broker)
//
//	tx, _ := pool.Begin(ctx)
//	// ... business writes on tx ...
//	_ = queue.PushTx(ctx, client, tx, SendEmail, payload) // intent row, same tx
//	_ = tx.Commit(ctx)                                    // job exists iff commit
//
//	relay, _ := outbox.NewRelay(store, baseBroker)        // run under ops/supervisor
//
// The relay claims committed rows in batches under a lease, pushes them to
// the broker, and deletes them on success. Rows are forwarded immediately
// regardless of Job.RunAt — the queue engine owns delay semantics. A row whose push fails is retried
// with capped exponential backoff and is never dropped: outbox delivery is
// guaranteed, and a broker outage resolves by draining the backlog once the
// broker returns. Delivery downstream is at-least-once — a relay crash
// between push and delete, or a lease lost mid-push, redelivers the row, and
// the queue engine's contract (idempotent handlers, eventbus inbox) already
// absorbs duplicates.
//
// MemoryStore is the in-process test double; production supplies a
// transactional Store implementation. Multiple relay instances share one store
// safely: claims are leased, and the worst case of a contended row is a
// duplicate push.
//
// Multi-tenant apps need no outbox-level seam: the tenant scope is captured
// into the job envelope by the producing queue.Client or eventbus (fail-closed
// there), and outbox stores and forwards the envelope verbatim.
package outbox
