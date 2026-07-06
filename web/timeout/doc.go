// Package timeout is cooperative per-request deadline middleware.
//
// New puts a deadline on every request context via context.WithTimeout and
// answers 504 (application/problem+json) when a ctx-respecting handler
// returns without writing a response after the deadline expired.
// Enforcement is cooperative, not preemptive: a handler that ignores its
// context keeps running in its own goroutine after the middleware writes
// the 504 and returns. The deadline only reaches a handler that actually
// reads r.Context().Done() or passes the context down to context-aware
// calls (database queries, outbound HTTP, etc.).
//
// A handler that already committed a response (wrote a header or body)
// before the deadline fired is left alone — the middleware checks
// middleware.WrapWriter(w).Wrote() and never rewrites a response that has
// already started.
//
// A custom WithResponder receives the failure error, and the problem package
// echoes err.Error() into the response Detail field for sub-500 statuses, so
// a responder that wraps a sensitive error should not pass it through
// unchanged — the default ErrTimeout is a generic 504 and is not echoed.
//
// # Streaming exemption
//
// Do not wrap streaming routes (SSE, long-poll, chunked transfer): a fixed
// deadline is wrong for a connection that is expected to stay open. Compose
// with middleware.Skip to exempt them by request predicate:
//
//	mw, err := timeout.New(timeout.WithConfig(cfg))
//	if err != nil {
//		// invalid Config (non-positive Timeout)
//	}
//	guarded := middleware.Skip(func(r *http.Request) bool {
//		return strings.HasPrefix(r.URL.Path, "/events")
//	}, mw)
//	handler := middleware.Wrap(mux, guarded)
//
// # Usage
//
//	mw, err := timeout.New(timeout.WithConfig(timeout.Config{Timeout: 15 * time.Second}))
//	if err != nil {
//		// invalid Config
//	}
//	handler := middleware.Wrap(mux, mw)
package timeout
