package eventrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/comms/webhook"
)

// WebhookDeliverer ships batches as HMAC-signed webhooks through a
// webhook.Sender: the same JSON-array body as HTTPDeliverer, signed by the
// sender's scheme, with the event ID as the idempotency key on single-event
// deliveries. The endpoint (URL + secret) is consumer data.
type WebhookDeliverer struct {
	sender   *webhook.Sender
	endpoint webhook.Endpoint
}

// NewWebhookDeliverer builds a WebhookDeliverer signing with sender and
// delivering to ep. Panics on a nil sender — wiring, not runtime data; the
// endpoint is validated by the sender on every delivery, and an invalid one
// is a permanent failure.
func NewWebhookDeliverer(sender *webhook.Sender, ep webhook.Endpoint) *WebhookDeliverer {
	if sender == nil {
		panic("eventrouter: NewWebhookDeliverer requires a sender")
	}
	return &WebhookDeliverer{sender: sender, endpoint: ep}
}

// Deliver signs and POSTs the batch, mapping the sender's status classes onto
// the router's: 2xx nil, webhook.ErrPermanentStatus and an invalid endpoint
// Permanent, transient statuses and transport failures retryable.
func (w *WebhookDeliverer) Deliver(ctx context.Context, events []Event) error {
	if w == nil || w.sender == nil { // zero deliverer bypassed NewWebhookDeliverer
		return errors.New("eventrouter: webhook deliverer not constructed with NewWebhookDeliverer")
	}
	body, err := json.Marshal(events)
	if err != nil {
		return Permanent(fmt.Errorf("eventrouter: encode batch: %w", err))
	}
	key := ""
	if len(events) == 1 {
		key = events[0].ID
	}
	_, err = w.sender.Send(ctx, w.endpoint, body, key)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, webhook.ErrPermanentStatus), errors.Is(err, webhook.ErrInvalidEndpoint):
		return Permanent(err)
	default:
		return err
	}
}
