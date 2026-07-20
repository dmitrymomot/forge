// Package eventbus is typed publish/subscribe over the queue engine: an
// event is a fact that already happened, and every named subscription
// reacts to it independently.
//
// Two modes share one wiring API. The durable bus (New, over any
// queue.Broker) fans each published event out as one job per subscription —
// each subscription is its own queue, so competing worker instances share a
// subscription's load and a slow subscription only delays itself — with the
// queue engine's at-least-once delivery, retry, backoff, and per-subscription
// dead-lettering. The sync bus (NewSync) invokes handlers in-process during
// Publish with no durability: tests, dev, and apps that can afford loss.
// Swapping modes changes one constructor call.
//
// # Usage
//
//	var UserCreated = eventbus.NewEvent[UserCreatedPayload]("user.created")
//
//	bus := eventbus.New(broker)
//	eventbus.Subscribe(bus, UserCreated, "send_welcome", func(ctx context.Context, d eventbus.Delivery[UserCreatedPayload]) error {
//		return mailer.SendWelcome(ctx, d.Payload.Email)
//	})
//	eventbus.Subscribe(bus, UserCreated, "provision", provisionHandler, eventbus.WithMaxAttempts(5))
//
//	svc, _ := eventbus.NewService(bus)   // run under ops/supervisor
//	err := eventbus.Publish(ctx, bus, UserCreated, UserCreatedPayload{...})
//
// Publishing an event with no subscriptions delivers nothing: publishers
// never couple to subscriber existence. Subscription handlers return the same
// verdicts as queue handlers — nil completes, queue.SkipRetry dead-letters
// poison input, queue.Cancel discards a moot event, any other error retries.
// The sync bus honors queue.Cancel as success too, but has no retry or
// dead-letter: every other handler error joins Publish's returned error.
//
// # Transactional publish and the idempotency inbox
//
// On brokers with real multi-document transactions (queue.TxPusher — pgqueue,
// mongoqueue), PublishTx fans out inside the caller's own transaction: the
// event is published if and only if the business writes commit. Brokers
// without it (redis) get transactional publish via async/outbox instead.
//
// Delivery is at-least-once, so HANDLERS MUST BE IDEMPOTENT. Every delivery
// of one published event — across retries and across subscriptions — carries
// the same Delivery.ID; the Inbox seam turns that into exactly-once side
// effects: Seen(ctx, tx, d.ID) inside the handler's transaction claims the id
// or reports it already processed. MemoryInbox is the test double;
// async/eventbus/postgres ships the transactional implementation.
//
// Multi-tenant apps configure eventbus.WithScope on the bus (captures the
// tenant into every fanned-out job, fail-closed) and pass
// queue.WithScopeContext to NewService (restores it into the handler
// context). Single-tenant apps configure neither.
package eventbus
