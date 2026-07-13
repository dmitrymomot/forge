package oauthserver

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/crypto/token"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// authCode is the sealed authorization-code payload. Codes are
// crypto/token blobs — deliberately not JWTs — so they can never be
// mistaken for access tokens; single-use is enforced by a SetNX claim on
// JTI at redemption.
type authCode struct {
	JTI         string `json:"jti"`
	ClientID    string `json:"cid"`
	RedirectURI string `json:"ru"`
	Subject     string `json:"sub"`
	Scope       string `json:"scp,omitempty"`
	Nonce       string `json:"n,omitempty"`
	Challenge   string `json:"cc"`
}

// Server issues OAuth2 tokens to registered clients: client_credentials
// for M2M partners and authorization_code (+PKCE) for first-party trusted
// apps. It is NOT a third-party IdP: no consent, no dynamic registration,
// no discovery metadata.
type Server struct {
	store         Store
	clk           clock.Clock
	codeStore     cache.Store
	signer        *jwt.Signer
	scope         func(ctx context.Context) (string, error)
	authenticator func(w http.ResponseWriter, r *http.Request) (string, bool)
	codes         *token.Codec[authCode]
	userClaims    func(ctx context.Context, subject string) (map[string]any, error)
	idgen         id.Prefix
	cfg           Config
	dummyHash     []byte
	codeTTL       time.Duration
}

// New builds a Server. signer provides the JWT keys (serve its JWKS() next
// to the token endpoint); store is the client registry.
func New(signer *jwt.Signer, store Store, opts ...Option) (*Server, error) {
	if signer == nil {
		return nil, fmt.Errorf("%w: signer required", ErrInvalidConfig)
	}
	if store == nil {
		return nil, fmt.Errorf("%w: store required", ErrInvalidConfig)
	}
	sc := serverConfig{cfg: DefaultConfig(), clk: clock.System(), codeTTL: time.Minute}
	for _, o := range opts {
		o(&sc)
	}
	if sc.cfg.TokenTTL <= 0 {
		sc.cfg.TokenTTL = DefaultConfig().TokenTTL
	}
	if sc.codeTTL <= 0 {
		sc.codeTTL = time.Minute
	}
	if err := sc.cfg.Validate(); err != nil {
		return nil, err
	}
	s := &Server{
		signer:        signer,
		store:         store,
		cfg:           sc.cfg,
		clk:           sc.clk,
		scope:         sc.scope,
		authenticator: sc.authenticator,
		codeStore:     sc.codeStore,
		userClaims:    sc.userClaims,
		codeTTL:       sc.codeTTL,
		idgen:         id.NewPrefix("client"),
		// dummyHash burns the same digest-compare time for unknown client
		// ids so they are indistinguishable from bad secrets.
		dummyHash: digest.SHA256([]byte("oauthserver:no-such-client")),
	}
	if sc.codeKeyset != nil {
		codes, err := token.FromKeyset[authCode](sc.codeKeyset,
			token.WithTTL(sc.codeTTL),
			token.WithPurpose("oauthserver:code"),
			token.WithClock(sc.clk),
		)
		if err != nil {
			return nil, err
		}
		s.codes = codes
	}
	return s, nil
}
