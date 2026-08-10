// Package fanout is the in-process publish/subscribe hub behind forge's
// realtime packages: ephemeral, at-most-once fan-out of byte payloads to
// currently-connected subscribers. Delivery never blocks a publisher — every
// subscriber owns a bounded buffer with an explicit overflow policy — and
// nothing survives a restart. Durable, at-least-once messaging is async/
// eventbus; fanout is the push side of live UI: SSE streams, WebSocket
// broadcasts, presence.
//
// # Usage
//
//	hub, _ := fanout.New()
//	defer hub.Close()
//
//	sub, _ := hub.Subscribe(ctx, []string{"chat.42"})
//	defer sub.Close()
//	go func() {
//		for msg := range sub.C() {
//			render(msg.Topic, msg.Payload)
//		}
//	}()
//
//	_ = hub.Publish(ctx, "chat.42", []byte(`{"text":"hi"}`))
//
// Publishing to a topic with no subscribers delivers nothing and is not an
// error. Payloads are delivered by reference: subscribers must not mutate
// Message.Payload.
//
// # Slow consumers
//
// A subscriber that does not drain its buffer never blocks the hub. The
// overflow policy — hub-wide via WithDefaultPolicy, per-subscription via
// WithPolicy — decides what happens instead: DropOldest (default) evicts the
// oldest buffered message, DropNewest discards the incoming one, CloseSlow
// tears the subscription down (its channel closes and Err reports
// ErrSlowConsumer). Every dropped message is counted on
// Subscription.Dropped.
//
// # Replay and resume
//
// WithReplay(n) keeps a per-topic ring of the last n messages so a
// reconnecting client can resume: Subscribe with WithResumeAfter(id) first
// delivers the buffered messages with ID greater than id, in order, then goes
// live with no gap. Message IDs are monotonic per hub instance — they are the
// Last-Event-ID currency for realtime/sse, not cross-instance or
// cross-restart stable. Rings of topics with no subscribers are retained for
// WithReplayTTL (default 5m) and then swept.
//
// # Multi-instance backplanes
//
// The Bus seam extends the hub across instances. With WithBus configured,
// Publish routes through the bus only, and every instance — including the
// publishing one — delivers to its local subscribers from the bus receive
// path, so ordering and loss behavior are identical everywhere. Consumers
// implement the Bus seam over their own transport — Postgres LISTEN/NOTIFY,
// Redis Pub/Sub, or similar — and must keep the driver's receive loop
// running for delivery to work.
//
// Multi-tenant apps configure WithScope; the returned tenant namespaces every
// topic, so subscribers in one tenant can never observe another tenant's
// messages — including across a shared Bus. Fail-closed: once configured, a
// hook error or empty scope fails Publish and Subscribe with
// ErrScopeMissing. Single-tenant apps do not configure it.
package fanout
