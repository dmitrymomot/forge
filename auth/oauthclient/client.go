package oauthclient

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/token"
	"github.com/dmitrymomot/forge/web/httpclient"
)

// Client drives authorization-code + PKCE login flows against registered
// providers. It is stateless: flow state rides a sealed crypto/token blob
// (cookie or caller-held), so any instance can complete any flow.
type Client struct {
	clk        clock.Clock
	codec      *token.Codec[flowState]
	providers  map[string]Provider
	source     func(ctx context.Context, name string) (Provider, error)
	binding    func(ctx context.Context) (string, error)
	hc         *http.Client
	verifiers  sync.Map // issuer\x00jwks\x00clientID -> *jwt.Verifier
	redirect   string
	cookieName string
	flowTTL    time.Duration
}

// New builds a Client. ks signs flow tokens (rotation-aware).
func New(ks *keyset.Keyset, opts ...Option) (*Client, error) {
	cfg := clientConfig{
		providers:  map[string]Provider{},
		clk:        clock.System(),
		cookieName: "oauth_flow",
		flowTTL:    10 * time.Minute,
	}
	for _, o := range opts {
		o(&cfg)
	}
	for name, p := range cfg.providers {
		if name == "" {
			return nil, fmt.Errorf("%w: empty provider name", ErrInvalidConfig)
		}
		if err := p.validate(); err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
	}
	codec, err := token.FromKeyset[flowState](ks,
		token.WithTTL(cfg.flowTTL),
		token.WithPurpose("oauthclient:flow"),
		token.WithClock(cfg.clk),
	)
	if err != nil {
		return nil, err
	}
	hc := cfg.hc
	if hc == nil {
		hc = httpclient.New(httpclient.WithTimeout(15 * time.Second))
	}
	return &Client{
		codec:      codec,
		providers:  cfg.providers,
		source:     cfg.source,
		binding:    cfg.binding,
		hc:         hc,
		clk:        cfg.clk,
		redirect:   cfg.redirectURL,
		cookieName: cfg.cookieName,
		flowTTL:    cfg.flowTTL,
	}, nil
}

// FromConfig builds a Client from an env-loaded Config.
func FromConfig(cfg Config, opts ...Option) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ks, err := keyset.New(keyset.WithBase64Keys(cfg.Keys))
	if err != nil {
		return nil, fmt.Errorf("%w: keys: %v", ErrInvalidConfig, err)
	}
	base := []Option{
		WithRedirectURL(cfg.RedirectURL),
		WithCookieName(cfg.CookieName),
		WithFlowTTL(cfg.FlowTTL),
	}
	return New(ks, append(base, opts...)...)
}

// resolve finds a provider: static registry first, then the source.
func (c *Client) resolve(ctx context.Context, name string) (Provider, error) {
	if p, ok := c.providers[name]; ok {
		return p, nil
	}
	if c.source != nil {
		p, err := c.source(ctx, name)
		if err != nil {
			return Provider{}, fmt.Errorf("oauthclient: provider source: %w", err)
		}
		if err := p.validate(); err != nil {
			return Provider{}, err
		}
		return p, nil
	}
	return Provider{}, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
}

// verifierFor returns a cached alg-pinned verifier for p's id_tokens.
func (c *Client) verifierFor(p Provider) (*jwt.Verifier, error) {
	key := p.Issuer + "\x00" + p.JWKSURL + "\x00" + p.ClientID
	if v, ok := c.verifiers.Load(key); ok {
		return v.(*jwt.Verifier), nil
	}
	v, err := jwt.NewVerifier(
		jwt.WithJWKSURL(p.JWKSURL, jwt.WithHTTPClient(c.hc)),
		jwt.WithIssuer(p.Issuer),
		jwt.WithAudience(p.ClientID),
		jwt.WithClock(c.clk),
	)
	if err != nil {
		return nil, err
	}
	actual, _ := c.verifiers.LoadOrStore(key, v)
	return actual.(*jwt.Verifier), nil
}
