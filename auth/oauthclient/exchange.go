package oauthclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/crypto/token"
)

// Result is a completed login.
type Result struct {
	// Token is the raw provider token response, exposed once. Persisting it
	// for later provider-API access is consumer domain.
	Token    TokenResponse
	Provider string
	ReturnTo string
	Identity Identity
}

// Exchange completes a flow: validates state (and tenancy binding) against
// the sealed flow token, exchanges the code with PKCE, and verifies the
// identity (id_token or Identity hook). callback is the full query the
// provider redirected back with.
func (c *Client) Exchange(ctx context.Context, flowToken string, callback url.Values) (*Result, error) {
	fs, err := c.codec.Parse(flowToken)
	if err != nil {
		if errors.Is(err, token.ErrExpired) {
			return nil, ErrFlowExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrFlowExpired, err)
	}
	if ec := callback.Get("error"); ec != "" {
		return nil, &ProviderError{Code: ec, Description: callback.Get("error_description")}
	}
	if st := callback.Get("state"); st == "" || !consttime.StringEqual(st, fs.State) {
		return nil, ErrStateMismatch
	}
	if c.binding != nil {
		b, err := c.binding(ctx)
		if err != nil {
			return nil, fmt.Errorf("oauthclient: scope hook: %w", err)
		}
		if b != fs.Binding {
			return nil, ErrScopeBinding
		}
	} else if fs.Binding != "" {
		return nil, ErrScopeBinding
	}
	p, err := c.resolve(ctx, fs.Provider)
	if err != nil {
		return nil, err
	}
	code := callback.Get("code")
	if code == "" {
		return nil, &ProviderError{Code: "invalid_response", Description: "callback missing code"}
	}
	redirect := p.RedirectURL
	if redirect == "" {
		redirect = c.redirect
	}
	tok, err := c.exchangeCode(ctx, p, code, fs.Verifier, redirect)
	if err != nil {
		return nil, err
	}
	var ident Identity
	if p.Identity != nil {
		ident, err = p.Identity(ctx, c.hc, tok)
	} else {
		ident, err = c.verifyIDToken(ctx, p, tok, fs.Nonce)
	}
	if err != nil {
		return nil, err
	}
	ident.Provider = fs.Provider
	return &Result{Identity: ident, Token: tok, Provider: fs.Provider, ReturnTo: fs.ReturnTo}, nil
}

// exchangeCode POSTs the authorization code to the provider token endpoint.
func (c *Client) exchangeCode(ctx context.Context, p Provider, code, verifier, redirect string) (TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauthclient: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // GitHub answers urlencoded without it
	resp, err := c.hc.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauthclient: token exchange: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("oauthclient: token exchange: %w", err)
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return TokenResponse{}, &ProviderError{Code: "invalid_response", Description: resp.Status}
	}
	if raw.Error != "" {
		return TokenResponse{}, &ProviderError{Code: raw.Error, Description: raw.ErrorDesc}
	}
	if resp.StatusCode != http.StatusOK || raw.AccessToken == "" {
		return TokenResponse{}, &ProviderError{Code: "invalid_response", Description: resp.Status}
	}
	tr := TokenResponse{
		AccessToken:  raw.AccessToken,
		TokenType:    raw.TokenType,
		RefreshToken: raw.RefreshToken,
		IDToken:      raw.IDToken,
		Scope:        raw.Scope,
	}
	if raw.ExpiresIn > 0 {
		tr.ExpiresAt = c.clk.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}
	return tr, nil
}

// verifyIDToken checks the id_token (signature via provider JWKS, iss, aud,
// exp inside jwt.Verify; nonce here) and maps claims to Identity.
func (c *Client) verifyIDToken(ctx context.Context, p Provider, tok TokenResponse, nonce string) (Identity, error) {
	if tok.IDToken == "" {
		return Identity{}, ErrNoIdentity
	}
	v, err := c.verifierFor(p)
	if err != nil {
		return Identity{}, err
	}
	claims, err := jwt.Verify[map[string]any](ctx, v, tok.IDToken)
	if err != nil {
		return Identity{}, err
	}
	raw := *claims
	got := str(raw["nonce"])
	if nonce == "" || !consttime.StringEqual(got, nonce) {
		return Identity{}, ErrNonceMismatch
	}
	ident := Identity{
		Subject:       str(raw["sub"]),
		Email:         str(raw["email"]),
		EmailVerified: boolClaim(raw["email_verified"]),
		Name:          str(raw["name"]),
		Picture:       str(raw["picture"]),
		Raw:           raw,
	}
	if ident.Subject == "" {
		return Identity{}, ErrNoIdentity
	}
	return ident, nil
}

// boolClaim tolerates IdPs that encode email_verified as a string.
func boolClaim(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	}
	return false
}
