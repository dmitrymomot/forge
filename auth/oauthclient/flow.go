package oauthclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/crypto/digest"
)

// flowState is the sealed per-flow blob: everything Complete/Exchange needs
// to finish a flow started by Begin/AuthURL.
type flowState struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Verifier string `json:"v"`
	Nonce    string `json:"n,omitempty"`
	Binding  string `json:"b,omitempty"`
	ReturnTo string `json:"r,omitempty"`
}

// Flow is a started login flow: the provider URL to send the user to and
// the sealed flow token the caller must present back to Exchange. Begin
// handles both via a cookie; SPA/mobile callers carry FlowToken themselves.
type Flow struct {
	URL       string
	FlowToken string
}

// BeginOption configures one Begin/AuthURL call.
type BeginOption func(*beginConfig)

type beginConfig struct {
	returnTo string
}

// WithReturnTo round-trips a post-login destination through the flow; it
// comes back verbatim in Result.ReturnTo.
func WithReturnTo(path string) BeginOption {
	return func(c *beginConfig) { c.returnTo = path }
}

// AuthURL starts a flow: resolves the provider, generates state + PKCE
// verifier (+ OIDC nonce), seals them into a flow token, and returns the
// provider authorize URL. Transport-neutral core under Begin.
func (c *Client) AuthURL(ctx context.Context, provider string, opts ...BeginOption) (*Flow, error) {
	p, err := c.resolve(ctx, provider)
	if err != nil {
		return nil, err
	}
	var bc beginConfig
	for _, o := range opts {
		o(&bc)
	}
	redirect := p.RedirectURL
	if redirect == "" {
		redirect = c.redirect
	}
	if redirect == "" {
		return nil, fmt.Errorf("%w: no redirect URL for provider %q", ErrInvalidConfig, provider)
	}
	fs := flowState{
		Provider: provider,
		State:    random.URLSafe(32),
		Verifier: random.URLSafe(32), // 43 chars encoded — within RFC 7636's 43..128
		ReturnTo: bc.returnTo,
	}
	if p.Identity == nil {
		fs.Nonce = random.URLSafe(16)
	}
	if c.binding != nil {
		b, err := c.binding(ctx)
		if err != nil {
			return nil, fmt.Errorf("oauthclient: scope hook: %w", err)
		}
		fs.Binding = b
	}
	tok, err := c.codec.Issue(fs)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, v := range p.AuthParams {
		if reservedParams[k] {
			return nil, fmt.Errorf("%w: %q", ErrReservedParam, k)
		}
		q.Set(k, v)
	}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirect)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(p.Scopes, " "))
	q.Set("state", fs.State)
	if fs.Nonce != "" {
		q.Set("nonce", fs.Nonce)
	}
	q.Set("code_challenge", pkceChallenge(fs.Verifier))
	q.Set("code_challenge_method", "S256")
	return &Flow{URL: p.AuthURL + "?" + q.Encode(), FlowToken: tok}, nil
}

// pkceChallenge is the RFC 7636 S256 transform.
func pkceChallenge(verifier string) string {
	return base64.RawURLEncoding.EncodeToString(digest.SHA256([]byte(verifier)))
}
