package guard

import (
	"strings"

	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	responder  problem.Responder
	challenge  string
	realm      string
	extractors []Extractor
	optional   bool
}

// Option configures New and BasicAuth. Options that don't apply to a
// constructor are ignored by it (each option documents which constructor
// reads it).
type Option func(*config)

// WithExtractors replaces the default extractor chain (BearerHeader only).
// Extractors run in order and the first hit wins — later extractors are not
// fallbacks for a credential that fails verification. Panics on an empty
// list: a guard that can never find a credential is a wiring bug. BasicAuth
// ignores this option (its scheme is fixed).
func WithExtractors(xs ...Extractor) Option {
	if len(xs) == 0 {
		panic("guard: WithExtractors requires at least one extractor")
	}
	return func(c *config) { c.extractors = xs }
}

// WithOptional lets requests without a credential pass through anonymously
// (From reports ok=false). A present-but-invalid credential still gets 401 —
// silently ignoring bad tokens masks expired sessions and probing. BasicAuth
// ignores this option.
func WithOptional() Option {
	return func(c *config) { c.optional = true }
}

// WithResponder overrides the rejection response (default problem.JSON 401).
// The error passed to the responder matches guard.ErrNoCredential or
// guard.ErrInvalidCredential via errors.Is; verify failures also match the
// verifier's own error.
//
// The error passed to a custom responder carries the verifier's own message
// (so errors.Is matches both the guard sentinel and the verifier error). A
// responder that renders err.Error() into the client response —
// problem.JSON does this for 4xx — therefore leaks that message. To keep
// the "no verifier detail to the client" guarantee, render only a generic
// message or the guard sentinel (as New's default responder does), not the
// raw error.
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

// WithChallenge sets the WWW-Authenticate header value emitted with every
// 401, e.g. `Bearer realm="api"` (RFC 6750). Default: none. BasicAuth
// ignores this option — it always emits its own Basic challenge.
func WithChallenge(v string) Option {
	return func(c *config) { c.challenge = v }
}

// WithRealm sets the Basic Auth realm (default "restricted"). Panics on
// quotes, backslashes, or control characters — the realm is echoed into the
// WWW-Authenticate header. New ignores this option.
func WithRealm(realm string) Option {
	if strings.ContainsAny(realm, "\"\\") || strings.ContainsFunc(realm, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		panic("guard: realm must not contain quotes, backslashes, or control characters")
	}
	return func(c *config) { c.realm = realm }
}
