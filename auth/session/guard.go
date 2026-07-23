package session

import (
	"context"
	"net/http"

	"github.com/dmitrymomot/forge/auth/guard"
)

// Extractor reports an authenticated session to guard. It returns the session
// id rather than the credential: the session was already loaded and validated
// by Middleware, so pairing it with Verifier costs no second store read and
// never repeats a transport's cookie or header name.
func Extractor() guard.Extractor {
	return func(r *http.Request) (string, bool) {
		s, ok := fromContext(r.Context())
		if !ok || !s.Authenticated() {
			return "", false
		}
		return s.rec.ID.String(), true
	}
}

type verifierOptions struct {
	identity func(*Session) guard.Identity
}

// VerifierOption configures Verifier.
type VerifierOption func(*verifierOptions)

// WithIdentity maps a session to a guard.Identity, letting roles or scopes ride
// a session namespace instead of a per-request store read. The default emits
// Subject and Method only — session core knows nothing about roles.
func WithIdentity(fn func(*Session) guard.Identity) VerifierOption {
	return func(o *verifierOptions) { o.identity = fn }
}

// Verifier adapts the session loaded by Middleware into a guard.Verifier, so
// guard owns authentication gating and session stays out of it. Mount it with
// Extractor; the credential argument is ignored because the session in context
// is already validated.
func Verifier(m *Manager, opts ...VerifierOption) guard.Verifier {
	var o verifierOptions
	for _, opt := range opts {
		opt(&o)
	}
	return guard.VerifierFunc(func(ctx context.Context, _ string) (guard.Identity, error) {
		s, ok := fromContext(ctx)
		if !ok {
			return guard.Identity{}, ErrNoSession
		}
		if !s.Authenticated() {
			return guard.Identity{}, ErrAnonymous
		}
		if o.identity != nil {
			return o.identity(s), nil
		}
		return guard.Identity{Subject: s.rec.UserID, Method: guard.MethodSession}, nil
	})
}
