package transport

import (
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
)

// jwtClaims wraps the opaque session token in registered JWT claims. The
// token rides a private claim; exp mirrors the session deadline so standard
// JWT validation rejects what the server would refuse anyway.
type jwtClaims struct {
	SessionToken string `json:"sid"`
	jwt.Claims
}

// JWTTransport wraps the session token in a signed JWT riding the
// Authorization Bearer credential. Build it with JWT.
type JWTTransport struct {
	verifier *jwt.Verifier
	signer   *jwt.Signer
	bearer   BearerTransport
	issuer   string
}

// JWTOption configures a JWTTransport.
type JWTOption func(*JWTTransport)

// WithJWTIssuer stamps iss on issued JWTs. Pair it with jwt.WithIssuer on
// the Verifier to enforce it.
func WithJWTIssuer(iss string) JWTOption { return func(t *JWTTransport) { t.issuer = iss } }

// WithJWTResponseHeader overrides the response header carrying freshly
// issued JWTs (default X-Session-Token).
func WithJWTResponseHeader(name string) JWTOption {
	return func(t *JWTTransport) { t.bearer.header = name }
}

// JWT returns a transport for infrastructure that already speaks JWT (edge
// gateways, service meshes): the opaque session token travels inside a
// signed JWT presented as an Authorization Bearer credential, and fresh
// JWTs are answered in a response header (default X-Session-Token) exactly
// like Bearer. The JWT is transport dressing only — the server-side session
// stays the source of truth, so rotation and revocation keep working, and
// exp mirrors the session deadline.
func JWT(signer *jwt.Signer, verifier *jwt.Verifier, opts ...JWTOption) *JWTTransport {
	t := &JWTTransport{signer: signer, verifier: verifier, bearer: BearerTransport{header: defaultTokenHeader}}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Extract verifies the presented JWT and returns the wrapped session token;
// missing, malformed, tampered, or expired JWTs extract "".
func (t *JWTTransport) Extract(r *http.Request) string {
	raw := t.bearer.Extract(r)
	if raw == "" {
		return ""
	}
	claims, err := jwt.Verify[jwtClaims](r.Context(), t.verifier, raw)
	if err != nil {
		return ""
	}
	return claims.SessionToken
}

// Embed signs a fresh JWT wrapping token, with exp = expiresAt, into the
// response header.
func (t *JWTTransport) Embed(w http.ResponseWriter, token string, expiresAt time.Time) error {
	signed, err := t.signer.Sign(jwtClaims{
		Claims: jwt.Claims{
			Issuer:    t.issuer,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		SessionToken: token,
	})
	if err != nil {
		return err
	}
	return t.bearer.Embed(w, signed, expiresAt)
}

// Clear writes an empty response header — the client's signal to drop its
// stored JWT.
func (t *JWTTransport) Clear(w http.ResponseWriter) error { return t.bearer.Clear(w) }
