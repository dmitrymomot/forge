package eventrouter

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/comms/postback"
)

// PostbackDeliverer fires tracker macro-URL postbacks through a
// postback.Sender: one unsigned ping per event, values extracted by a
// registered Go function. Trackers take one ping per conversion, so pair it
// with WithBatchSize(1); it still handles larger batches by firing each event
// in turn and reporting the worst outcome (permanent outranks transient, so
// poison isolation can split the batch into precise per-event verdicts).
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

// Deliver fires one postback per event and classifies the joined outcome:
// nil when every ping landed, Permanent when any event failed permanently (a
// values error, a non-2xx non-5xx status, or an invalid destination — the
// router then isolates per event), otherwise the joined transient errors.
func (p *PostbackDeliverer) Deliver(ctx context.Context, events []Event) error {
	if p == nil || p.sender == nil || p.values == nil { // zero deliverer bypassed NewPostbackDeliverer
		return errors.New("eventrouter: postback deliverer not constructed with NewPostbackDeliverer")
	}
	var transient, permanent []error
	for i := range events {
		vals, err := p.values(events[i])
		if err != nil {
			permanent = append(permanent, fmt.Errorf("event %s: values: %w", events[i].ID, err))
			continue
		}
		if _, err := p.sender.Send(ctx, p.dest, vals); err != nil {
			wrapped := fmt.Errorf("event %s: %w", events[i].ID, err)
			switch {
			case errors.Is(err, postback.ErrClientStatus), errors.Is(err, postback.ErrInvalidTemplate):
				permanent = append(permanent, wrapped)
			default: // 5xx and transport failures
				transient = append(transient, wrapped)
			}
		}
	}
	if len(permanent) > 0 {
		return Permanent(errors.Join(append(permanent, transient...)...))
	}
	return errors.Join(transient...)
}
