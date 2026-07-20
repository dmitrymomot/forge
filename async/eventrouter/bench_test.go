package eventrouter_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/eventrouter"
)

type nopDeliverer struct{}

func (nopDeliverer) Deliver(context.Context, []eventrouter.Event) error { return nil }

// BenchmarkPublishRouted measures the full sync-bus publish → route → deliver
// path with batching disabled: envelope marshal, handler dispatch, payload
// re-marshal, and the destination's singleton fast path.
func BenchmarkPublishRouted(b *testing.B) {
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("bench.routed")
	dest := eventrouter.NewDestination("nop", nopDeliverer{}, eventrouter.WithBatchSize(1))
	eventrouter.Route(bus, evt, dest)
	ctx := context.Background()
	p := payload{V: "bench"}

	b.ReportAllocs()
	for b.Loop() {
		if err := eventbus.Publish(ctx, bus, evt, p); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPublishRoutedBatched measures the batching path under concurrent
// publishers: joins coordinate through the destination mutex and flush by
// size.
func BenchmarkPublishRoutedBatched(b *testing.B) {
	bus := eventbus.NewSync()
	evt := eventbus.NewEvent[payload]("bench.batched")
	dest := eventrouter.NewDestination("nop", nopDeliverer{},
		eventrouter.WithBatchSize(8), eventrouter.WithBatchAge(time.Millisecond))
	eventrouter.Route(bus, evt, dest)
	ctx := context.Background()
	p := payload{V: "bench"}

	b.ReportAllocs()
	var wg sync.WaitGroup
	for b.Loop() {
		wg.Add(8)
		for range 8 {
			go func() {
				defer wg.Done()
				_ = eventbus.Publish(ctx, bus, evt, p)
			}()
		}
		wg.Wait()
	}
}

// BenchmarkBatchEncode measures the wire encoding the HTTP and webhook
// deliverers pay per flush.
func BenchmarkBatchEncode(b *testing.B) {
	events := make([]eventrouter.Event, 100)
	for i := range events {
		events[i] = eventrouter.Event{
			OccurredAt: time.Now().UTC(),
			Payload:    json.RawMessage(`{"order_id":"ord_123","total_cents":4200}`),
			ID:         "evt_0123456789abcdef",
			Name:       "order.placed",
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := json.Marshal(events); err != nil {
			b.Fatal(err)
		}
	}
}
