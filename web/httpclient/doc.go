// Package httpclient builds a resilient *http.Client (the stdlib type) from a
// RoundTripper stack over the shipped retry/backoff/circuitbreaker packages:
// static + context-derived headers and before/after hooks, jittered retry of
// idempotent methods on transient failures (5xx/429/network, honoring
// Retry-After), a per-attempt timeout, and an OPT-IN per-host circuit breaker.
//
//	client := httpclient.New(
//		httpclient.WithPerAttemptTimeout(2*time.Second),
//		httpclient.WithContextHeaders(func(ctx context.Context) http.Header {
//			h := http.Header{}
//			if id, ok := requestid.From(ctx); ok {
//				h.Set("X-Request-ID", id)
//			}
//			return h
//		}),
//		httpclient.WithBreakerGroup(), // enable per-host breaker
//	)
//
//	resp, err := client.Get(url)
//	if err != nil { /* transport/breaker error */ }
//	if err := problem.Decode(resp); err != nil { /* 4xx/5xx problem+json */ }
//
// Retry is on by default (3 attempts) for GET/HEAD/PUT/DELETE/OPTIONS only —
// POST is excluded to avoid silent double-submits (override with
// WithRetryMethods). New returns the stdlib *http.Client, so problem surfacing
// is the companion problem.Decode call rather than a changed Do signature.
package httpclient
