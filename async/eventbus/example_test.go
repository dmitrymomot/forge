package eventbus_test

import (
	"context"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/queue"
)

type orderPlaced struct {
	OrderID string `json:"order_id"`
}

var evtOrderPlaced = eventbus.NewEvent[orderPlaced]("example.order_placed")

func Example() {
	broker := queue.NewMemoryBroker()
	bus := eventbus.New(broker)

	done := make(chan struct{}, 2)
	eventbus.Subscribe(bus, evtOrderPlaced, "receipt", func(_ context.Context, d eventbus.Delivery[orderPlaced]) error {
		fmt.Println("receipt for", d.Payload.OrderID)
		done <- struct{}{}
		return nil
	})
	eventbus.Subscribe(bus, evtOrderPlaced, "fulfillment", func(_ context.Context, d eventbus.Delivery[orderPlaced]) error {
		fmt.Println("fulfillment for", d.Payload.OrderID)
		done <- struct{}{}
		return nil
	})

	cfg := queue.DefaultConfig()
	cfg.PollInterval = 10 * time.Millisecond
	svc, err := eventbus.NewService(bus, queue.WithConfig(cfg))
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = svc.Run(ctx); close(stopped) }()

	_ = eventbus.Publish(context.Background(), bus, evtOrderPlaced, orderPlaced{OrderID: "ord_1"})

	<-done
	<-done
	cancel()
	<-stopped
	// Unordered output:
	// receipt for ord_1
	// fulfillment for ord_1
}

func ExampleNewSync() {
	bus := eventbus.NewSync()
	eventbus.Subscribe(bus, evtOrderPlaced, "audit", func(_ context.Context, d eventbus.Delivery[orderPlaced]) error {
		fmt.Println("audited", d.Payload.OrderID)
		return nil
	})

	_ = eventbus.Publish(context.Background(), bus, evtOrderPlaced, orderPlaced{OrderID: "ord_2"})
	// Output: audited ord_2
}
