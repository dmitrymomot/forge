package eventrouter

import (
	"context"
	"encoding/json"
	"time"
)

// Event is the unit of egress a route hands to a destination: the stable
// event identity plus the outbound payload (the remapped form when the route
// remaps, the published payload otherwise). ID is the same on every delivery
// of one published event — across retries, batches, and destinations — so it
// is the key receivers dedup on. The JSON tags are the wire form the
// HTTP and webhook deliverers batch-encode.
type Event struct {
	OccurredAt time.Time       `json:"occurred_at"`
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// Deliverer ships one batch to an external destination and classifies the
// outcome: nil when the destination accepted it, a Permanent-wrapped error
// when retrying cannot help (the router dead-letters, isolating the poison
// event first on multi-event batches), and any other error for transient
// failures (every event in the batch retries on the queue's backoff).
//
// Deliver is called concurrently when batches overlap, and the events slice
// is only valid for the duration of the call.
type Deliverer interface {
	Deliver(ctx context.Context, events []Event) error
}
