package eventrouter

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/comms/postback"
)

// PostbackDeliverer fires tracker macro-URL postbacks through a
// postback.Sender: one unsigned ping per event, values extracted by a
// registered Go function. Trackers take one ping per conversion, so Deliver
// only ever fires singletons: a multi-event batch is refused with a Permanent
// error before anything fires, which makes the router's poison-isolation pass
// deliver each event alone with its own precise verdict. Partial batch
// outcomes therefore cannot re-fire already-accepted pings — any batch size
// works; WithBatchSize(1) merely skips the refused batch attempt.
type PostbackDeliverer struct {
	sender *postback.Sender
	values func(e Event) (map[string]string, error)
	dest   postback.Destination
}

// NewPostbackDeliverer builds a PostbackDeliverer rendering dest against the
// macro values fn extracts from each event. A fn error marks the event
// permanently undeliverable — poison, not transient. Include the stable
// Event.ID as a macro value so the tracker can dedup redeliveries. Panics on
// a nil sender or fn — wiring, not runtime data.
func NewPostbackDeliverer(sender *postback.Sender, dest postback.Destination, fn func(e Event) (map[string]string, error)) *PostbackDeliverer {
	if sender == nil {
		panic("eventrouter: NewPostbackDeliverer requires a sender")
	}
	if fn == nil {
		panic("eventrouter: NewPostbackDeliverer requires a values function")
	}
	return &PostbackDeliverer{sender: sender, dest: dest, values: fn}
}

// Deliver fires the single event's postback and classifies the outcome: nil
// for 2xx, Permanent for a values error, a non-2xx non-5xx status, or an
// invalid destination, otherwise the transient error (5xx and transport
// failures — worth retrying). A multi-event batch is refused with Permanent
// without firing anything: pings are per-event, and the refusal routes the
// batch through poison isolation so every event still fires exactly once.
func (p *PostbackDeliverer) Deliver(ctx context.Context, events []Event) error {
	if p == nil || p.sender == nil || p.values == nil { // zero deliverer bypassed NewPostbackDeliverer
		return errors.New("eventrouter: postback deliverer not constructed with NewPostbackDeliverer")
	}
	if len(events) != 1 {
		// Firing the batch here would couple every event's verdict to its
		// batchmates: one transient failure would retry — and re-ping — the
		// events the tracker already accepted. Refusing before any I/O hands
		// the batch to the router's isolation pass instead.
		return Permanent(fmt.Errorf("eventrouter: postbacks are per-event, got a batch of %d", len(events)))
	}
	e := events[0]
	vals, err := p.values(e)
	if err != nil {
		return Permanent(fmt.Errorf("eventrouter: event %s: values: %w", e.ID, err))
	}
	if _, err := p.sender.Send(ctx, p.dest, vals); err != nil {
		switch {
		case errors.Is(err, postback.ErrClientStatus), errors.Is(err, postback.ErrInvalidTemplate):
			return Permanent(fmt.Errorf("eventrouter: event %s: %w", e.ID, err))
		default: // 5xx and transport failures
			return fmt.Errorf("eventrouter: event %s: %w", e.ID, err)
		}
	}
	return nil
}
