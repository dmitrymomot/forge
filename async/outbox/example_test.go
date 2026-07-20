package outbox_test

import (
	"context"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/async/outbox"
	"github.com/dmitrymomot/forge/async/queue"
)

type orderPlaced struct {
	OrderID string `json:"order_id"`
}

var kindOrderPlaced = queue.NewKind[orderPlaced]("example.order_placed")

// Example wires the full outbox pipeline over in-memory pieces: PushTx writes
// an intent row through the wrapped broker, the relay forwards it into the
// real broker after "commit", and a plain queue worker processes it. In
// production the store is pgoutbox over the business database, the broker is
// one without native transactional push (redis), and tx is the caller's
// pgx.Tx.
func Example() {
	store := outbox.NewMemoryStore()
	inner := queue.NewMemoryBroker()
	broker := outbox.Wrap(store, inner)

	// Producer side: intent row instead of a direct push. The memory store
	// ignores tx; pgoutbox requires the caller's pgx.Tx here.
	client := queue.NewClient(broker)
	if err := queue.PushTx(context.Background(), client, nil, kindOrderPlaced, orderPlaced{OrderID: "ord_1"}); err != nil {
		panic(err)
	}

	// Relay side: forward committed rows into the real broker.
	cfg := outbox.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	relay, err := outbox.NewRelay(store, inner, outbox.WithConfig(cfg))
	if err != nil {
		panic(err)
	}

	// Worker side: a plain queue service over the same broker.
	qcfg := queue.DefaultConfig()
	qcfg.PollInterval = 10 * time.Millisecond
	svc, err := queue.NewService(broker, queue.WithConfig(qcfg))
	if err != nil {
		panic(err)
	}
	done := make(chan struct{})
	queue.Register(svc, kindOrderPlaced, func(_ context.Context, p orderPlaced) error {
		fmt.Println("processing", p.OrderID)
		close(done)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{}, 2)
	go func() { _ = relay.Run(ctx); stopped <- struct{}{} }()
	go func() { _ = svc.Run(ctx); stopped <- struct{}{} }()

	<-done
	cancel()
	<-stopped
	<-stopped
	// Output: processing ord_1
}
