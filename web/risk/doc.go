// Package risk is a generic anti-fraud gate: consumer-supplied scorer
// functions each return a fraud probability in [0, 1] for an input, a
// combining strategy folds them into one score, and a threshold gate decides
// whether the call proceeds. The package owns combining, gating, and error
// flow only — all fraud logic (useragent checks, fingerprint mismatch,
// velocity, geo rules) lives in consumer scorers, typically wired from
// web/useragent, web/fingerprint, web/geoip, and web/clientip.
//
// An Engine is built once with New and is immutable and concurrent-safe.
// Check runs every scorer in registration order, combines the scores
// (default strategy Max — fraud signals are not additive, one strong signal
// must not be diluted by weak ones), and trips when the combined score
// reaches the gate (score >= gate). On trip without an OnFraud handler,
// Check returns a *FraudError matching errors.Is(err, ErrFraud) and carrying
// per-scorer attribution. With a handler, the handler's return decides:
// nil proceeds (shadow mode, divert-and-allow), an error blocks with that
// error verbatim.
//
// Everything fails closed: a scorer error, a NaN, or a score outside [0, 1]
// fails the check — no silent skips, no clamping of broken scorers. Score
// computes the combined score without gating, for telemetry and for tuning
// the gate in shadow mode before enforcing.
//
//	engine, err := risk.New[Visit](
//		risk.WithScorer(botUserAgent),     // func(ctx, Visit) (float64, error)
//		risk.WithScorer(fingerprintMismatch),
//		risk.WithGate(0.8),
//	)
//	if err != nil { ... }
//	if err := engine.Check(ctx, visit); err != nil { ... } // gate tripped
//
// Middleware adapts an Engine to net/http: buildInput assembles the scorer
// input from the request, gate trips and scorer failures alike reject with
// 403 (override with WithRejectHandler to divert to a decoy or render a
// problem response). Off-request use — queue consumers, decision decorators
// (a wrapper calling Check and returning a decoy decision on fraud), OnHit
// shadow scoring — calls Check or Score directly.
//
// Tenancy is a passed value, not a seam: the engine is pure compute with no
// storage, so tenant identity travels inside the input type T for scorers to
// read, or the consumer builds one engine per tenant.
package risk
