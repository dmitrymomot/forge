// Package eventrouter is event egress over async/eventbus: it routes
// published events to external destinations — analytics warehouses, partner
// endpoints, affiliate trackers — with per-destination isolation, batched
// delivery, and queue-grade retry.
//
// Each Route binds one event to one Destination as its own named eventbus
// subscription, so every (event, destination) pair is a separate durable
// queue: a slow or failing destination only delays itself. Filtering and
// remapping are registered Go functions on the route — never a mapping DSL.
// Events reaching a destination accumulate into a batch flushed by size or
// age; the delivering jobs block until the flush resolves, so nothing is
// acknowledged before the destination accepted it.
//
// # Usage
//
//	warehouse, _ := eventrouter.NewHTTPDeliverer("https://collect.example.com/batch",
//		eventrouter.WithHTTPHeader("Authorization", "Bearer "+token))
//	dest := eventrouter.NewDestination("warehouse", warehouse,
//		eventrouter.WithBatchSize(200), eventrouter.WithBatchAge(2*time.Second))
//
//	eventrouter.Route(bus, OrderPlaced, dest)
//	eventrouter.Route(bus, UserCreated, dest,
//		eventrouter.WithFilter(func(d eventbus.Delivery[UserCreatedPayload]) bool {
//			return !d.Payload.Internal
//		}),
//		eventrouter.WithRemap(func(d eventbus.Delivery[UserCreatedPayload]) (any, error) {
//			return warehouseUser{ID: d.Payload.ID, Plan: d.Payload.Plan}, nil
//		}))
//
// Routes are startup wiring on the same bus the app publishes to; the
// eventbus Service drains them alongside every other subscription. On a sync
// bus (eventbus.NewSync) routes work too, but Publish blocks until the batch
// flushes — pair sync buses with WithBatchSize(1).
//
// # Delivery semantics
//
// Delivery is at-least-once and the router never dedups: suppressing a
// redelivery in the router would trade a duplicate for silent loss. Stable
// event IDs ride every delivery — an "id" field on every batched event and
// an Idempotency-Key header on single-event deliveries — and receivers dedup
// on them (the Stripe contract).
//
// A batch resolves to per-event queue verdicts. Success acknowledges every
// job in the batch. A transient failure retries every job on the queue's
// backoff — events re-batch on redelivery. A permanent failure (an error
// marked with Permanent) on a multi-event batch triggers poison isolation:
// each event is re-delivered alone, so the poison event dead-letters by
// itself instead of taking its batchmates with it — already-accepted events
// may be re-sent, which receivers dedup. A permanent failure on a
// single-event batch dead-letters that event without burning retry attempts,
// as do filter-independent poison conditions: a failing remap and an
// unmarshalable remapped payload.
//
// # Deliverers
//
// A Deliverer ships one batch and classifies the outcome: nil for accepted,
// Permanent-wrapped for rejections retrying cannot fix, any other error for
// transient failures. Deliver is called concurrently when batches overlap.
// Two reference adapters ship in the package: NewHTTPDeliverer (generic
// JSON-batch POST) and NewWebhookDeliverer (HMAC-signed deliveries via
// comms/webhook). Destination configs — URLs, secrets, macro templates —
// are consumer data; forge ships the engine, never a connector catalog.
//
// # Tenancy
//
// Multi-tenant apps configure WithScope on the destination with a hook
// reading the tenant from the handler context (restored by
// queue.WithScopeContext on the eventbus Service): batches are keyed by
// scope and never mix tenants, and a configured hook fails closed — a
// missing scope is a delivery error, never a cross-tenant batch.
// Single-tenant apps configure nothing.
package eventrouter
