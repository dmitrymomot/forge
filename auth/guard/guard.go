package guard

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// Identity is the authenticated principal a Verifier resolved. Subject is
// never empty on a successful verification; Tenant, Scopes, and Meta are
// optional. Scopes is carried for the future authorization decision seam
// (401-vs-403 split) — guard itself never reads it.
type Identity struct {
	Meta    map[string]string // verifier-specific extras (email, key id, …)
	Subject string            // principal id — never empty on success
	Tenant  string            // optional tenant id
	Method  string            // how the request authenticated: "bearer", "session", "apikey", "basic"
	Scopes  []string          // permissions/scopes for the future authz seam
}

// Verifier turns an extracted credential into an Identity. A returned error
// means the credential is rejected: the middleware answers 401 and never
// surfaces error detail to the client. Implementations must be safe for
// concurrent use.
type Verifier interface {
	Verify(ctx context.Context, credential string) (Identity, error)
}

// VerifierFunc adapts a function to Verifier. It doubles as the package's
// test fake — provider adapters (auth/jwt, auth/apikey, auth/session) are
// closures over it or their own types.
type VerifierFunc func(ctx context.Context, credential string) (Identity, error)

// Verify implements Verifier.
func (f VerifierFunc) Verify(ctx context.Context, credential string) (Identity, error) {
	return f(ctx, credential)
}

// New returns middleware that authenticates every request through v: the
// extractor chain (default BearerHeader) finds the credential, v resolves
// it to an Identity stored in request context for From/MustFrom. A request
// with no credential gets 401 (or passes anonymously with WithOptional); a
// rejected credential gets 401 always. Rejections go through the responder
// (default problem.JSON 401) after setting WWW-Authenticate when a
// challenge is configured. New panics on a nil verifier — a wiring bug.
func New(v Verifier, opts ...Option) middleware.Middleware {
	if v == nil {
		panic("guard: nil verifier")
	}
	jsonResponder := problem.JSON(problem.WithStatus(http.StatusUnauthorized))
	cfg := config{
		// The default responder never lets a verifier's error text reach the
		// client: it collapses the wrapped chain down to the guard-level
		// sentinel before handing off to problem.JSON, which otherwise puts
		// err.Error() straight into the response body for any 4xx status.
		// Custom responders (WithResponder) still see the full wrapped
		// chain via errors.Is.
		responder: func(w http.ResponseWriter, r *http.Request, err error) {
			safe := ErrInvalidCredential
			if errors.Is(err, ErrNoCredential) {
				safe = ErrNoCredential
			}
			jsonResponder(w, r, safe)
		},
		extractors: []Extractor{BearerHeader()},
	}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cred, found := extract(cfg.extractors, r)
			if !found {
				if cfg.optional {
					next.ServeHTTP(w, r)
					return
				}
				cfg.reject(w, r, ErrNoCredential)
				return
			}
			id, err := v.Verify(r.Context(), cred)
			if err != nil {
				cfg.reject(w, r, fmt.Errorf("%w: %w", ErrInvalidCredential, err))
				return
			}
			if id.Subject == "" {
				// A successful verification with no Subject is a verifier
				// bug; treat it as a rejection rather than admitting an
				// anonymous principal.
				cfg.reject(w, r, ErrInvalidCredential)
				return
			}
			next.ServeHTTP(w, r.WithContext(identityKey.With(r.Context(), id)))
		})
	}
}

func extract(xs []Extractor, r *http.Request) (string, bool) {
	for _, x := range xs {
		if cred, ok := x(r); ok {
			return cred, true
		}
	}
	return "", false
}

func (c config) reject(w http.ResponseWriter, r *http.Request, err error) {
	if c.challenge != "" {
		w.Header().Set("WWW-Authenticate", c.challenge)
	}
	c.responder(w, r, err)
}
