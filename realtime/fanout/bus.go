package fanout

import "context"

// Bus is the multi-instance backplane seam. A hub built with WithBus routes
// every Publish through Bus.Publish and delivers to its local subscribers
// only from the bus receive path, so all instances observe one ordering and
// one loss profile.
//
// Contract for implementations: Publish broadcasts the message to every
// instance's handler, including the publishing instance's own. Subscribe
// registers the single delivery callback, replacing any previous one — a bus
// instance backs exactly one hub. The callback must be invoked sequentially
// per bus instance; it is fast and non-blocking (bounded-buffer sends only).
// Delivery is at-most-once: messages published while an instance's receive
// loop is down are lost to that instance.
//
// Drivers: fanout/pgbus (Postgres LISTEN/NOTIFY), fanout/redisbus (Redis
// Pub/Sub). Both also implement supervisor.Service — their Run loop is the
// receive path.
type Bus interface {
	Publish(ctx context.Context, topic string, payload []byte) error
	Subscribe(fn func(topic string, payload []byte))
}
