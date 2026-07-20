package eventrouter_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/eventrouter"
)

type signupPayload struct {
	Email    string `json:"email"`
	Internal bool   `json:"internal"`
}

var evtSignup = eventbus.NewEvent[signupPayload]("example.signup")

// printDeliverer stands in for a real adapter (NewHTTPDeliverer,
// NewWebhookDeliverer, NewPostbackDeliverer) so the example is runnable
// offline.
type printDeliverer struct{}

func (printDeliverer) Deliver(_ context.Context, events []eventrouter.Event) error {
	for _, e := range events {
		fmt.Printf("delivered %s %s\n", e.Name, e.Payload)
	}
	return nil
}

func Example() {
	bus := eventbus.NewSync() // production: eventbus.New(broker) + eventbus.NewService

	dest := eventrouter.NewDestination("warehouse", printDeliverer{},
		eventrouter.WithBatchSize(1)) // sync buses pair with size-1 batches

	eventrouter.Route(bus, evtSignup, dest,
		eventrouter.WithFilter(func(d eventbus.Delivery[signupPayload]) bool {
			return !d.Payload.Internal
		}),
		eventrouter.WithRemap(func(d eventbus.Delivery[signupPayload]) (any, error) {
			return map[string]string{"email": d.Payload.Email}, nil
		}),
	)

	ctx := context.Background()
	_ = eventbus.Publish(ctx, bus, evtSignup, signupPayload{Email: "ada@example.com"})
	_ = eventbus.Publish(ctx, bus, evtSignup, signupPayload{Email: "ops@corp.test", Internal: true})

	// Output:
	// delivered example.signup {"email":"ada@example.com"}
}
