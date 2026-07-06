// Package secheaders is middleware that sets response security headers, with an
// opt-in typed Content-Security-Policy and per-request CSP nonce.
//
// Every response gets a safe baseline: X-Content-Type-Options: nosniff,
// Referrer-Policy: strict-origin-when-cross-origin, X-Frame-Options: DENY, and
// Cross-Origin-Opener-Policy: same-origin. Each is set with setIfEmpty
// semantics — a header already present (from earlier middleware or the handler)
// is never overwritten, so the handler always wins.
//
// # HSTS is opt-in
//
// Strict-Transport-Security is off unless Config.HSTSMaxAge is positive, since
// it pins clients to HTTPS and is unsafe to enable before TLS is in place. Set
// it (and includeSubDomains) via WithConfig, typically loaded from the
// SECURITY_HEADERS_ env prefix.
//
// # CSP is opt-in
//
// A Content-Security-Policy that breaks a page is worse than none, so CSP is
// emitted only when WithCSP supplies a typed Policy. Policy is code-shaped, not
// env-shaped: express it in Go with the Self/None/Data/Blob source constants
// rather than hand-serializing a string.
//
// # Per-request nonce
//
// WithNonce mints a fresh base64url nonce per request, appends it to the
// script-src and style-src directives, and exposes it via Nonce(ctx). Use it to
// allow specific inline scripts/styles without 'unsafe-inline'. In a templ
// component, read it straight off the request context:
//
//	<script nonce={ secheaders.Nonce(ctx) }>...</script>
//
// Outside the middleware (or when WithNonce is not set), Nonce returns "".
//
// # Usage
//
//	mw, err := secheaders.New(
//		secheaders.WithConfig(cfg), // env-loaded HSTS / FrameOptions
//		secheaders.WithCSP(secheaders.Policy{
//			DefaultSrc: []string{secheaders.Self},
//			ScriptSrc:  []string{secheaders.Self},
//		}),
//		secheaders.WithNonce(),
//	)
//	if err != nil {
//		// invalid Config (unknown FrameOptions, negative HSTS)
//	}
//	handler := middleware.Wrap(mux, mw)
package secheaders
