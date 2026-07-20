package eventbus_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/queue"
)

type benchPayload struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

var benchEvent = eventbus.NewEvent[benchPayload]("bench.user_created")

func noopHandler(context.Context, eventbus.Delivery[benchPayload]) error { return nil }

func syncBus(subs int) *eventbus.Bus {
	bus := eventbus.NewSync()
	for i := range subs {
		eventbus.Subscribe(bus, benchEvent, fmt.Sprintf("sub_%d", i), noopHandler)
	}
	return bus
}

func BenchmarkSyncPublish(b *testing.B) {
	for _, subs := range []int{1, 4} {
		b.Run(fmt.Sprintf("subs_%d", subs), func(b *testing.B) {
			bus := syncBus(subs)
			ctx := context.Background()
			payload := benchPayload{UserID: "u_1", Email: "a@b.c"}
			b.ReportAllocs()
			for b.Loop() {
				if err := eventbus.Publish(ctx, bus, benchEvent, payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDurablePublish(b *testing.B) {
	for _, subs := range []int{1, 4} {
		b.Run(fmt.Sprintf("subs_%d", subs), func(b *testing.B) {
			bus := eventbus.New(queue.NewMemoryBroker())
			for i := range subs {
				eventbus.Subscribe(bus, benchEvent, fmt.Sprintf("sub_%d", i), noopHandler)
			}
			ctx := context.Background()
			payload := benchPayload{UserID: "u_1", Email: "a@b.c"}
			b.ReportAllocs()
			for b.Loop() {
				if err := eventbus.Publish(ctx, bus, benchEvent, payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMemoryInboxSeen(b *testing.B) {
	inbox := eventbus.NewMemoryInbox()
	ctx := context.Background()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		if _, err := inbox.Seen(ctx, nil, fmt.Sprintf("evt-%d", i)); err != nil {
			b.Fatal(err)
		}
	}
}
