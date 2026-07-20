package eventbus

import (
	"context"
	"errors"
	"sync"
)

// Inbox is the idempotency-inbox seam: it marks event ids as processed
// inside the consumer's own database transaction, so "record the event as
// handled" commits or rolls back atomically with the handler's writes. One
// Inbox per subscription — deliveries of the same event to different
// subscriptions carry the same Delivery.ID and each must process it once.
//
//	seen, err := inbox.Seen(ctx, tx, d.ID)
//	if err != nil { return err }
//	if seen { return nil } // duplicate delivery, already committed
//	// ... handler writes in the same tx ...
//
// tx is driver-specific, exactly as in queue.PushTx (the postgres inbox
// asserts pgx.Tx). Seen marks the id and reports whether it was already
// marked: false means this call claimed the first processing.
type Inbox interface {
	Seen(ctx context.Context, tx any, id string) (bool, error)
}

// MemoryInbox is the in-process Inbox test double: a plain set, tx ignored.
// It has no transactional semantics — a handler that marks an id and then
// fails keeps the mark even though its real writes rolled back, so a retry
// would skip the event. Tests and throwaway dev only; production consumers
// use a transactional inbox (async/eventbus/postgres) or bring their own.
type MemoryInbox struct {
	seen map[string]struct{}
	mu   sync.Mutex
}

// NewMemoryInbox builds an empty in-memory inbox.
func NewMemoryInbox() *MemoryInbox {
	return &MemoryInbox{seen: make(map[string]struct{})}
}

// Seen implements Inbox. The tx argument is ignored.
func (i *MemoryInbox) Seen(_ context.Context, _ any, id string) (bool, error) {
	if id == "" {
		return false, errors.New("eventbus: Seen requires a non-empty event id")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, ok := i.seen[id]; ok {
		return true, nil
	}
	i.seen[id] = struct{}{}
	return false, nil
}
