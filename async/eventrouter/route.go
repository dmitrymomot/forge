package eventrouter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/queue"
)

// routeConfig carries one route's per-event hooks.
type routeConfig[T any] struct {
	filter  func(d eventbus.Delivery[T]) bool
	remap   func(d eventbus.Delivery[T]) (any, error)
	subOpts []eventbus.SubscribeOption
}

// RouteOption configures a single Route call.
type RouteOption[T any] func(*routeConfig[T])

// WithFilter routes only deliveries fn approves; the rest are acknowledged
// without delivery. Panics on a nil fn.
func WithFilter[T any](fn func(d eventbus.Delivery[T]) bool) RouteOption[T] {
	if fn == nil {
		panic("eventrouter: WithFilter(nil)")
	}
	return func(r *routeConfig[T]) { r.filter = fn }
}

// WithRemap replaces the outbound payload: fn's result is marshaled into
// Event.Payload in place of the published payload. A remap error is poison —
// the event dead-letters on this route without burning retry attempts, since
// a deterministic mapping cannot heal by retrying. Panics on a nil fn.
func WithRemap[T any](fn func(d eventbus.Delivery[T]) (any, error)) RouteOption[T] {
	if fn == nil {
		panic("eventrouter: WithRemap(nil)")
	}
	return func(r *routeConfig[T]) { r.remap = fn }
}

// WithSubscribeOptions passes eventbus subscription options through to the
// route's subscription: retry backoff, attempt budget, and handler timeout
// tune exactly as on a hand-written subscription. The handler timeout bounds
// the whole join — batch wait plus delivery — so keep it well above
// WithBatchAge + WithDeliveryTimeout.
func WithSubscribeOptions[T any](opts ...eventbus.SubscribeOption) RouteOption[T] {
	return func(r *routeConfig[T]) { r.subOpts = append(r.subOpts, opts...) }
}

// Route binds evt to dest on bus as the named subscription dest.Name(): every
// published evt that passes the route's filter joins the destination's batch
// as one Event, remapped when the route remaps. Routes are startup wiring —
// declare them before building the eventbus Service; Route panics (via
// eventbus.Subscribe) on a duplicate (event, destination) pair.
func Route[T any](bus *eventbus.Bus, evt eventbus.Event[T], dest *Destination, opts ...RouteOption[T]) {
	if dest == nil {
		panic(fmt.Sprintf("eventrouter: Route(%q) with nil destination", evt.Name()))
	}
	var r routeConfig[T]
	for _, opt := range opts {
		opt(&r)
	}
	eventbus.Subscribe(bus, evt, dest.name, func(ctx context.Context, d eventbus.Delivery[T]) error {
		if r.filter != nil && !r.filter(d) {
			return nil
		}
		var payload any = d.Payload
		if r.remap != nil {
			out, err := r.remap(d)
			if err != nil {
				return queue.SkipRetry(fmt.Errorf("eventrouter: remap %q for %q: %w", d.Name, dest.name, err))
			}
			payload = out
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return queue.SkipRetry(fmt.Errorf("eventrouter: marshal %q for %q: %w", d.Name, dest.name, err))
		}
		return dest.join(ctx, Event{
			OccurredAt: d.OccurredAt,
			Payload:    raw,
			ID:         d.ID,
			Name:       d.Name,
		})
	}, r.subOpts...)
}
