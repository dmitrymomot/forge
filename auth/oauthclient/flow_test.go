package oauthclient_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// testKeyset takes testing.TB so Task 6's benchmarks can reuse it.
func testKeyset(tb testing.TB) *keyset.Keyset {
	tb.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(tb, err)
	return ks
}

func oidcProvider() oauthclient.Provider {
	return oauthclient.Provider{
		ClientID: "cid", ClientSecret: "sec",
		AuthURL:  "https://idp.example.com/authorize",
		TokenURL: "https://idp.example.com/token",
		JWKSURL:  "https://idp.example.com/jwks",
		Issuer:   "https://idp.example.com",
		Scopes:   []string{"openid", "email"},
	}
}

func TestAuthURLContents(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)

	flow, err := c.AuthURL(context.Background(), "idp", oauthclient.WithReturnTo("/dash"))
	require.NoError(t, err)
	require.NotEmpty(t, flow.FlowToken)

	u, err := url.Parse(flow.URL)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(flow.URL, "https://idp.example.com/authorize?"))
	q := u.Query()
	assert.Equal(t, "cid", q.Get("client_id"))
	assert.Equal(t, "https://app.example.com/cb", q.Get("redirect_uri"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "openid email", q.Get("scope"))
	assert.NotEmpty(t, q.Get("state"))
	assert.NotEmpty(t, q.Get("nonce"), "OIDC provider gets a nonce")
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
}

func TestAuthURLProviderRedirectOverridesAndAuthParams(t *testing.T) {
	p := oidcProvider()
	p.RedirectURL = "https://tenant.example.com/cb"
	p.AuthParams = map[string]string{"prompt": "select_account"}
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", p))
	require.NoError(t, err)
	flow, err := c.AuthURL(context.Background(), "idp")
	require.NoError(t, err)
	u, _ := url.Parse(flow.URL)
	assert.Equal(t, "https://tenant.example.com/cb", u.Query().Get("redirect_uri"))
	assert.Equal(t, "select_account", u.Query().Get("prompt"))
}

func TestAuthURLNoNonceForIdentityHookProvider(t *testing.T) {
	p := oauthclient.GitHub(oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"})
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("github", p))
	require.NoError(t, err)
	flow, err := c.AuthURL(context.Background(), "github")
	require.NoError(t, err)
	u, _ := url.Parse(flow.URL)
	assert.Empty(t, u.Query().Get("nonce"))
}

func TestNewRejectsReservedAuthParams(t *testing.T) {
	p := oidcProvider()
	p.AuthParams = map[string]string{"state": "evil"}
	_, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProvider("idp", p))
	require.ErrorIs(t, err, oauthclient.ErrReservedParam)
}

func TestAuthURLUnknownProvider(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t), oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "nope")
	require.ErrorIs(t, err, oauthclient.ErrUnknownProvider)
}

func TestAuthURLProviderSource(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProviderSource(func(ctx context.Context, name string) (oauthclient.Provider, error) {
			if name == "tenant-okta" {
				return oidcProvider(), nil
			}
			return oauthclient.Provider{}, errors.New("no such tenant idp")
		}))
	require.NoError(t, err)

	_, err = c.AuthURL(context.Background(), "tenant-okta")
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "missing")
	require.Error(t, err, "source errors propagate (fail-closed)")
}

func TestAuthURLScopeHookFailClosed(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t),
		oauthclient.WithRedirectURL("https://a/cb"),
		oauthclient.WithProvider("idp", oidcProvider()),
		oauthclient.WithScope(func(ctx context.Context) (string, error) { return "", errors.New("no tenant") }))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "idp")
	require.Error(t, err)
}

func TestAuthURLNoRedirectAnywhere(t *testing.T) {
	c, err := oauthclient.New(testKeyset(t), oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "idp")
	require.ErrorIs(t, err, oauthclient.ErrInvalidConfig)
}

func TestConfigValidate(t *testing.T) {
	cfg := oauthclient.DefaultConfig()
	require.Error(t, cfg.Validate(), "Keys required")
	cfg.Keys = "1:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // base64("0123456789abcdef0123456789abcdef")
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "oauth_flow", cfg.CookieName)
}

func TestFromConfig(t *testing.T) {
	cfg := oauthclient.DefaultConfig()
	cfg.Keys = "1:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // base64("0123456789abcdef0123456789abcdef")
	cfg.RedirectURL = "https://app.example.com/cb"
	c, err := oauthclient.FromConfig(cfg, oauthclient.WithProvider("idp", oidcProvider()))
	require.NoError(t, err)
	_, err = c.AuthURL(context.Background(), "idp")
	require.NoError(t, err)
}
