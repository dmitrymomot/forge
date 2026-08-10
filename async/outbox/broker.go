package outbox

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/async/queue"
)

// Broker decorates a queue.Broker with transactional enqueue: PushTx writes
// intent rows into the outbox Store inside the caller's transaction, and
// every other operation — including plain Push — delegates to the wrapped
// broker untouched. Wire it wherever the wrapped broker would go (producer
// Client and worker Service alike); pair it with a Relay over the same Store
// and broker so committed rows get forwarded.
type Broker struct {
	store Store
	next  queue.Broker
}

var (
	_ queue.Broker     = (*Broker)(nil)
	_ queue.TxPusher   = (*Broker)(nil)
	_ queue.Maintainer = (*Broker)(nil)
)

// Wrap decorates next with Store-backed transactional enqueue. Panics on a
// nil store or broker — this is startup wiring, and failing fast beats a
// producer that crashes on first push.
func Wrap(store Store, next queue.Broker) *Broker {
	if store == nil {
		panic("outbox: Wrap with nil store")
	}
	if next == nil {
		panic("outbox: Wrap with nil broker")
	}
	return &Broker{store: store, next: next}
}

// PushTx implements queue.TxPusher: the jobs become outbox rows inside the
// caller's transaction and reach the wrapped broker via the Relay after
// commit. tx must be what the Store expects (a driver asserts its own tx type).
// Note that wrapping shadows a native TxPusher on next — deliberate when the
// business data and the queue live in different databases, pointless
// otherwise.
func (b *Broker) PushTx(ctx context.Context, tx any, jobs ...queue.Job) error {
	return b.store.Add(ctx, tx, jobs...)
}

// Maintain implements queue.Maintainer by delegation, so wrapping does not
// silently disable the wrapped broker's housekeeping when the worker Service
// probes for the capability. A broker without it makes Maintain a no-op.
func (b *Broker) Maintain(ctx context.Context) error {
	if m, ok := b.next.(queue.Maintainer); ok {
		return m.Maintain(ctx)
	}
	return nil
}

func (b *Broker) Push(ctx context.Context, jobs ...queue.Job) error {
	return b.next.Push(ctx, jobs...)
}

func (b *Broker) Claim(ctx context.Context, queueName string, n int, lease time.Duration) ([]queue.ClaimedJob, error) {
	return b.next.Claim(ctx, queueName, n, lease)
}

func (b *Broker) Extend(ctx context.Context, id, token string, lease time.Duration) error {
	return b.next.Extend(ctx, id, token, lease)
}

func (b *Broker) Ack(ctx context.Context, id, token string) error {
	return b.next.Ack(ctx, id, token)
}

func (b *Broker) Nack(ctx context.Context, id, token string, retryAt time.Time, reason string) error {
	return b.next.Nack(ctx, id, token, retryAt, reason)
}

func (b *Broker) Kill(ctx context.Context, id, token string, reason string) error {
	return b.next.Kill(ctx, id, token, reason)
}

func (b *Broker) ListDead(ctx context.Context, queueName string, limit int) ([]queue.Job, error) {
	return b.next.ListDead(ctx, queueName, limit)
}

func (b *Broker) Requeue(ctx context.Context, id string) error {
	return b.next.Requeue(ctx, id)
}

func (b *Broker) Purge(ctx context.Context, id string) error {
	return b.next.Purge(ctx, id)
}

func (b *Broker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	return b.next.PurgeDeadBefore(ctx, cutoff)
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	return b.next.Stats(ctx)
}
