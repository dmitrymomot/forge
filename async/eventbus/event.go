package eventbus

import (
	"encoding/json"
	"time"
)

// Event binds an event name to its payload type T. Declare one package-level
// Event per event type and share it between publishers and subscribers: the
// name string exists in exactly one place, and payload type drift between
// Publish and Subscribe becomes a compile error.
//
//	var UserCreated = eventbus.NewEvent[UserCreatedPayload]("user.created")
type Event[T any] struct {
	name string
}

// NewEvent creates an Event for payload type T. The name must be non-empty
// and unique across the application (convention: "domain.past_tense", e.g.
// "user.created"). Panics on an empty name: events are package-level wiring,
// not runtime data.
func NewEvent[T any](name string) Event[T] {
	if name == "" {
		panic("eventbus: NewEvent requires a non-empty name")
	}
	return Event[T]{name: name}
}

// Name returns the event name.
func (e Event[T]) Name() string { return e.name }

// Delivery is what a subscription handler receives: the decoded payload plus
// the event metadata. ID is the stable event id — every subscription's
// delivery of one published event carries the same ID, so it is the dedup key
// for the Inbox (and the Idempotency-Key receivers should dedup on).
type Delivery[T any] struct {
	OccurredAt time.Time
	Payload    T
	ID         string
	Name       string
}

// wireVersion is the envelope encoding version. Bump only with a decoder
// that still accepts every previous version: durable events published by old
// binaries must stay deliverable.
const wireVersion = 1

// envelope is the wire form a published event travels in: it rides as the
// queue.Job payload of every fanned-out job, so all subscriptions see the
// same event id and timestamp.
type envelope struct {
	ID      string          `json:"id"`
	Name    string          `json:"n"`
	Payload json.RawMessage `json:"p,omitempty"`
	V       int             `json:"v"`
	AtMS    int64           `json:"at"`
}
