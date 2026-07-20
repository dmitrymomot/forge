package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

// Kind is the queue job kind for durable webhook deliveries: Enqueue pushes
// it, RegisterDeliverer handles it.
var Kind = queue.NewKind[Delivery]("webhook.deliver")

// Delivery is the queued job payload: which stored endpoint to deliver to
// (resolved to URL+secret at fire time, so secrets never sit in job rows),
// the event body, and the idempotency key reused across retries. The body
// survives the queue's JSON round trip semantically intact but not
// byte-for-byte (stdlib re-encoding compacts and HTML-escapes <, >, &);
// the signature is computed over the delivered bytes, so it always matches.
type Delivery struct {
	Endpoint string          `json:"endpoint"`
	Key      string          `json:"key,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}

// Enqueue pushes a delivery for durable dispatch. An empty Key gets a
// generated UUID so every retry of the job carries the same idempotency key.
// Returns ErrInvalidDelivery on an empty endpoint ID, a non-JSON payload, or
// a Key that cannot ride in an HTTP header (it would fail every attempt).
func Enqueue(ctx context.Context, c *queue.Client, d Delivery, opts ...queue.PushOption) error {
	if d.Endpoint == "" {
		return fmt.Errorf("%w: empty endpoint id", ErrInvalidDelivery)
	}
	if !json.Valid(d.Payload) {
		return fmt.Errorf("%w: payload is not valid JSON", ErrInvalidDelivery)
	}
	if !validKey(d.Key) {
		return fmt.Errorf("%w: key contains non-printable-ASCII bytes", ErrInvalidDelivery)
	}
	if d.Key == "" {
		d.Key = id.NewUUID().String()
	}
	return queue.Push(ctx, c, Kind, d, opts...)
}

// validKey reports whether key is printable ASCII (no spaces) — the subset
// that is always a valid HTTP header value. Idempotency keys are IDs, so the
// strictness costs nothing and catches a poisoned job before it burns its
// whole attempt budget on "invalid header field value" transport errors.
func validKey(key string) bool {
	for i := range len(key) {
		if key[i] <= ' ' || key[i] > '~' {
			return false
		}
	}
	return true
}

// Resolver maps a stored endpoint ID to its current URL and secret at fire
// time. Return ErrEndpointNotFound when the endpoint was deleted or disabled
// — the queued delivery is cancelled as moot. Any other error is treated as
// transient and the delivery retries. In multi-tenant apps the queue's scope
// context (queue.WithScopeContext) is already on ctx — scope the lookup there.
type Resolver func(ctx context.Context, endpointID string) (Endpoint, error)

// RegisterDeliverer binds the delivery handler on a queue worker: resolve the
// endpoint, send, and map the outcome to a queue verdict — nil or transient
// errors retry on the queue's backoff, ErrPermanentStatus and
// ErrInvalidEndpoint dead-letter without burning attempts, and
// ErrEndpointNotFound cancels the job. Panics on nil sender or resolver —
// wiring bugs, same contract as queue.Register.
func RegisterDeliverer(s *queue.Service, sender *Sender, resolve Resolver, opts ...queue.HandlerOption) {
	if sender == nil {
		panic("webhook: RegisterDeliverer with nil sender")
	}
	if resolve == nil {
		panic("webhook: RegisterDeliverer with nil resolver")
	}
	queue.Register(s, Kind, func(ctx context.Context, d Delivery) error {
		ep, err := resolve(ctx, d.Endpoint)
		switch {
		case errors.Is(err, ErrEndpointNotFound):
			return queue.Cancel
		case err != nil:
			return fmt.Errorf("webhook: resolve endpoint %q: %w", d.Endpoint, err)
		}
		_, err = sender.Send(ctx, ep, d.Payload, d.Key)
		if errors.Is(err, ErrPermanentStatus) || errors.Is(err, ErrInvalidEndpoint) {
			return queue.SkipRetry(err)
		}
		return err
	}, opts...)
}
