package oauthserver_test

// The mirror recipe, end to end: a real oauthclient logs a user in against
// a real oauthserver over HTTP — the exact wiring a white-label mirror or
// trusted first-party app uses (see both doc.go files).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

func TestMirrorLoginViaOauthclient(t *testing.T) {
	// --- central auth server (auth.platform.com) ---
	signer := testSigner(t)
	mux := http.NewServeMux()
	authSrv := httptest.NewServer(mux)
	t.Cleanup(authSrv.Close)

	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = authSrv.URL

	srv, err := oauthserver.New(signer, oauthserver.NewMemoryStore(),
		oauthserver.WithConfig(cfg),
		oauthserver.WithCodeStore(cacheStore(t)),
		oauthserver.WithCodeKeyset(testKeyset(t)),
		// Central session exists — the authenticator answers instantly,
		// which is exactly the SSO-across-mirrors behavior.
		oauthserver.WithAuthenticator(staticUser("user-42")),
		oauthserver.WithUserClaims(func(ctx context.Context, subject string) (map[string]any, error) {
			return map[string]any{"email": "u42@example.com", "email_verified": true, "name": "User 42"}, nil
		}),
	)
	require.NoError(t, err)

	authorize, err := srv.AuthorizeHandler()
	require.NoError(t, err)
	mux.Handle("GET /oauth/authorize", authorize)
	mux.Handle("POST /oauth/token", srv.TokenHandler())
	mux.Handle("GET /.well-known/jwks.json", signer.JWKS())

	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name:         "mirror-1",
		Grants:       []string{oauthserver.GrantAuthorizationCode},
		Scopes:       []string{"profile"},
		RedirectURIs: []string{"https://mirror1.example.com/auth/callback"},
	})
	require.NoError(t, err)

	// --- mirror app: oauthclient with a hand-built Provider (the recipe) ---
	flowKeys, err := keyset.New(keyset.WithPrimary(1, []byte("fedcba9876543210fedcba9876543210")))
	require.NoError(t, err)
	mirror, err := oauthclient.New(flowKeys,
		oauthclient.WithRedirectURL("https://mirror1.example.com/auth/callback"),
		oauthclient.WithHTTPClient(authSrv.Client()),
		oauthclient.WithProvider("platform", oauthclient.Provider{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			AuthURL:      authSrv.URL + "/oauth/authorize",
			TokenURL:     authSrv.URL + "/oauth/token",
			JWKSURL:      authSrv.URL + "/.well-known/jwks.json",
			Issuer:       authSrv.URL,
			Scopes:       []string{"profile"},
		}))
	require.NoError(t, err)

	// 1. mirror starts the flow
	flow, err := mirror.AuthURL(context.Background(), "platform", oauthclient.WithReturnTo("/lobby"))
	require.NoError(t, err)

	// 2. the "browser" follows the authorize URL; the server SSO-redirects
	//    straight back to the mirror callback with a code
	browser := &http.Client{
		Transport:     authSrv.Client().Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := browser.Get(flow.URL)
	require.NoError(t, err)
	//nolint:nilaway // resp is non-nil whenever err is nil, per http.Client.Do's contract
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusFound, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(loc.String(), "https://mirror1.example.com/auth/callback"),
		"authorize redirected to %s", loc)

	// 3. mirror completes the flow with the callback query
	res, err := mirror.Exchange(context.Background(), flow.FlowToken, loc.Query())
	require.NoError(t, err)
	assert.Equal(t, "user-42", res.Identity.Subject)
	assert.Equal(t, "u42@example.com", res.Identity.Email)
	assert.True(t, res.Identity.EmailVerified)
	assert.Equal(t, "User 42", res.Identity.Name)
	assert.Equal(t, "/lobby", res.ReturnTo)
	assert.Equal(t, "platform", res.Identity.Provider)
	assert.NotEmpty(t, res.Token.AccessToken)

	// 4. a second login reuses nothing from the first (fresh code, fresh state)
	flow2, err := mirror.AuthURL(context.Background(), "platform")
	require.NoError(t, err)
	resp2, err := browser.Get(flow2.URL)
	require.NoError(t, err)
	require.NoError(t, resp2.Body.Close())
	loc2, err := url.Parse(resp2.Header.Get("Location"))
	require.NoError(t, err)
	res2, err := mirror.Exchange(context.Background(), flow2.FlowToken, loc2.Query())
	require.NoError(t, err)
	assert.Equal(t, "user-42", res2.Identity.Subject)
}
