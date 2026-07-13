package oauthclient_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/core/clock"
)

func TestExchangeOIDCHappyPath(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c, oauthclient.WithReturnTo("/dash"))
	withNonce(f, authQ)

	res, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.NoError(t, err)
	assert.Equal(t, "idp", res.Provider)
	assert.Equal(t, "/dash", res.ReturnTo)
	assert.Equal(t, "user-1", res.Identity.Subject)
	assert.Equal(t, "u@example.com", res.Identity.Email)
	assert.True(t, res.Identity.EmailVerified)
	assert.Equal(t, "User One", res.Identity.Name)
	assert.Equal(t, "idp", res.Identity.Provider)
	assert.Equal(t, "at-123", res.Token.AccessToken)
	assert.False(t, res.Token.ExpiresAt.IsZero())
	assert.NotEmpty(t, res.Identity.Raw["iss"], "raw claims exposed")

	// the exchange POST carried the PKCE verifier and the code
	assert.Equal(t, "authorization_code", f.TokenForm.Get("grant_type"))
	assert.Equal(t, "code-1", f.TokenForm.Get("code"))
	assert.NotEmpty(t, f.TokenForm.Get("code_verifier"))
	assert.Equal(t, "https://app.example.com/cb", f.TokenForm.Get("redirect_uri"))
	assert.Equal(t, "cid", f.TokenForm.Get("client_id"))
	assert.Equal(t, "sec", f.TokenForm.Get("client_secret"))
}

func TestExchangeStateMismatch(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	withNonce(f, authQ)
	cb := callbackQuery(authQ, "code-1")
	cb.Set("state", "forged")
	_, err := c.Exchange(context.Background(), flow.FlowToken, cb)
	require.ErrorIs(t, err, oauthclient.ErrStateMismatch)
}

func TestExchangeProviderErrorCallback(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, _ := startFlow(t, c)
	cb := url.Values{"error": {"access_denied"}, "error_description": {"user cancelled"}}
	_, err := c.Exchange(context.Background(), flow.FlowToken, cb)
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "access_denied", perr.Code) //nolint:nilaway // perr is guaranteed non-nil by require.ErrorAs above
}

func TestExchangeExpiredFlow(t *testing.T) {
	f := newFakeOIDC(t)
	mock := clock.NewMock(time.Now())
	c := f.newClient(t, oauthclient.WithClock(mock))
	flow, authQ := startFlow(t, c)
	mock.Advance(11 * time.Minute)
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrFlowExpired)
}

// TestExchangeZeroFlowTTLClampedToDefault proves WithFlowTTL(0) does NOT
// disable flow expiry (crypto/token treats a zero token.WithTTL as "never
// expires"): New must clamp a non-positive flowTTL to the 10m default
// before building the flow codec.
func TestExchangeZeroFlowTTLClampedToDefault(t *testing.T) {
	f := newFakeOIDC(t)
	mock := clock.NewMock(time.Now())
	c := f.newClient(t, oauthclient.WithClock(mock), oauthclient.WithFlowTTL(0))
	flow, authQ := startFlow(t, c)
	mock.Advance(11 * time.Minute) // past the clamped 10m default
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrFlowExpired, "0 must clamp to the default TTL, not disable expiry")
}

func TestExchangeTamperedFlowToken(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	_, authQ := startFlow(t, c)
	_, err := c.Exchange(context.Background(), "garbage.token", callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrFlowExpired)
}

func TestExchangeNonceMismatch(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	f.IDTokenClaims["nonce"] = "wrong-nonce"
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrNonceMismatch)
}

// TestExchangeIDTokenWrongAudience proves the id_token's aud is pinned to
// the client's ClientID (verifierFor sets jwt.WithAudience(p.ClientID)): an
// id_token minted for a different audience must be rejected, guarding
// against id_token substitution if the pinning were ever dropped.
func TestExchangeIDTokenWrongAudience(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	withNonce(f, authQ) // isolate the failure to aud, not the nonce check
	f.IDTokenClaims["aud"] = "other-client"

	res, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.Error(t, err)
	require.ErrorIs(t, err, jwt.ErrAudienceMismatch)
	assert.Nil(t, res)
}

// TestExchangeIDTokenWrongIssuer proves the id_token's iss is pinned to the
// provider's configured Issuer (verifierFor sets jwt.WithIssuer(p.Issuer)):
// an id_token claiming a different issuer must be rejected.
func TestExchangeIDTokenWrongIssuer(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	withNonce(f, authQ) // isolate the failure to iss, not the nonce check
	f.IDTokenClaims["iss"] = "https://evil.example.com"

	res, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.Error(t, err)
	require.ErrorIs(t, err, jwt.ErrIssuerMismatch)
	assert.Nil(t, res)
}

func TestExchangeTokenEndpointRFCError(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	f.TokenStatus = 400
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "bad"))
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "invalid_grant", perr.Code) //nolint:nilaway // perr is guaranteed non-nil by require.ErrorAs above
}

func TestExchangeMissingCode(t *testing.T) {
	f := newFakeOIDC(t)
	c := f.newClient(t)
	flow, authQ := startFlow(t, c)
	cb := url.Values{"state": {authQ.Get("state")}}
	_, err := c.Exchange(context.Background(), flow.FlowToken, cb)
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "invalid_response", perr.Code) //nolint:nilaway // perr is guaranteed non-nil by require.ErrorAs above
}

func TestExchangeScopeBinding(t *testing.T) {
	f := newFakeOIDC(t)
	tenant := "tenant-a"
	hook := func(ctx context.Context) (string, error) { return tenant, nil }
	c := f.newClient(t, oauthclient.WithScope(hook))
	flow, authQ := startFlow(t, c)
	withNonce(f, authQ)

	tenant = "tenant-b" // flow finishes under a different tenant
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.ErrorIs(t, err, oauthclient.ErrScopeBinding)

	tenant = "tenant-a"
	_, err = c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.NoError(t, err)
}

func TestExchangeScopeHookErrorFailsClosed(t *testing.T) {
	f := newFakeOIDC(t)
	fail := false
	c := f.newClient(t, oauthclient.WithScope(func(ctx context.Context) (string, error) {
		if fail {
			return "", errors.New("boom")
		}
		return "t", nil
	}))
	flow, authQ := startFlow(t, c)
	fail = true
	_, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.Error(t, err)
	require.NotErrorIs(t, err, oauthclient.ErrScopeBinding, "hook error is not a binding mismatch")
}

func TestExchangeIdentityHookPath(t *testing.T) {
	// GitHub-shaped: no id_token; identity from the hook.
	gh := fakeGitHub(t, 200)
	f := newFakeOIDC(t)
	f.TokenBody = map[string]any{"access_token": "gho_token", "token_type": "bearer", "scope": "read:user"}
	p := f.provider()
	p.Issuer, p.JWKSURL = "", "" // not OIDC
	p.Identity = oauthclient.GitHubIdentity(gh.URL)
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", p),
		oauthclient.WithHTTPClient(f.Server.Client()))
	require.NoError(t, err)

	flow, authQ := startFlow(t, c)
	res, err := c.Exchange(context.Background(), flow.FlowToken, callbackQuery(authQ, "code-1"))
	require.NoError(t, err)
	assert.Equal(t, "12345", res.Identity.Subject)
	assert.Equal(t, "idp", res.Identity.Provider, "provider name comes from the registry key")
	assert.Empty(t, res.Token.IDToken)
}
