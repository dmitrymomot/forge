// Package webhook is the complete webhooks package, both directions: outbound
// HMAC-signed deliveries with durable retry over async/queue, and inbound
// signature-verification middleware. One pluggable Scheme seam carries both —
// Stripe (t=,v1=), GitHub (X-Hub-Signature-256), and Slack (v0:) ship
// built-in, and a bespoke partner scheme is one interface away.
//
// # Outbound
//
// A Sender POSTs a payload to an Endpoint (URL + signing secret), signed by
// its Scheme (default: Stripe-style under a neutral "Webhook-Signature"
// header) with an idempotency key in "Webhook-Id" so receivers dedup across
// retries. Send fires exactly one attempt and reports the outcome by class —
// nil for 2xx, ErrTransientStatus for 408/429/5xx, ErrPermanentStatus for
// any other status, transport failures as-is — the split a queue needs for
// its retry-vs-dead-letter decision.
//
// Durable delivery rides async/queue: Enqueue pushes a Delivery naming a
// stored endpoint by ID, and RegisterDeliverer binds the worker-side handler
// that resolves the endpoint through a consumer Resolver at fire time (URL
// and secret come from the consumer's store on every attempt — secrets never
// sit in job rows), sends, and maps outcomes to queue verdicts: transient
// failures retry on the queue's backoff, permanent ones dead-letter without
// burning attempts, and a deleted endpoint (ErrEndpointNotFound) cancels the
// job as moot.
//
// # Inbound
//
// Verify wraps a handler with fail-closed signature verification: the body is
// read capped (default 1 MiB) and restored so the handler reads it normally,
// the scheme authenticates it in constant time within a timestamp tolerance
// (default 5m), and any secret from the Secrets source may match — hand out
// several during rotation. Rejections answer problem+json built from a bare
// sentinel; scheme and lookup internals never reach the caller.
//
// # Non-goals
//
//   - No endpoint store or registry: endpoints, their secrets, and their
//     event subscriptions are consumer data behind the Resolver.
//   - No fan-out: which endpoints receive which event is a consumer query
//     that ends in one Enqueue per endpoint.
//   - No delivery-attempt log: persist Result consumer-side.
//   - No unsigned deliveries: an endpoint without a secret is rejected —
//     tracker-style unsigned pings are comms/postback.
//   - No tenant seam of its own: outbound scoping lives in the Resolver
//     (the queue's scope context is on ctx), inbound in the per-request
//     Secrets source.
//
// # Usage
//
// Outbound, durable:
//
//	sender := webhook.New()
//	webhook.RegisterDeliverer(svc, sender, func(ctx context.Context, id string) (webhook.Endpoint, error) {
//		return store.Endpoint(ctx, id) // -> webhook.ErrEndpointNotFound when deleted
//	})
//
//	err := webhook.Enqueue(ctx, client, webhook.Delivery{
//		Endpoint: "ep_123",
//		Payload:  json.RawMessage(`{"type":"invoice.paid","id":"evt_1"}`),
//	})
//
// Inbound:
//
//	mux.Handle("POST /hooks/github", webhook.Verify(
//		webhook.GitHub(),
//		webhook.StaticSecrets([]byte(os.Getenv("GITHUB_WEBHOOK_SECRET"))),
//	)(handler))
package webhook
