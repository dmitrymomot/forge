package guard

import "context"

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
