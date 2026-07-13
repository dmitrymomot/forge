package oauthserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthserver"
)

// ccClient registers a client_credentials client and returns its creds.
func ccClient(t *testing.T, srv *oauthserver.Server, scopes ...string) *oauthserver.ClientCredentials {
	t.Helper()
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "partner", Grants: []string{oauthserver.GrantClientCredentials}, Scopes: scopes,
	})
	require.NoError(t, err)
	return creds
}

// postToken POSTs form to the token handler; basic creds attach when set.
func postToken(t *testing.T, h http.Handler, form url.Values, basicID, basicSecret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicID != "" {
		req.SetBasicAuth(basicID, basicSecret)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

type ccClaims struct {
	jwt.Claims
	Scope    string `json:"scope"`
	ClientID string `json:"client_id"`
	Tenant   string `json:"tenant"`
}

func TestClientCredentialsBasicAuth(t *testing.T) {
	signer := testSigner(t)
	store := oauthserver.NewMemoryStore()
	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = "https://auth.example.com"
	cfg.Audience = "https://api.example.com"
	srv, err := oauthserver.New(signer, store, oauthserver.WithConfig(cfg))
	require.NoError(t, err)
	creds := ccClient(t, srv, "read:odds", "write:bets")

	rec := postToken(t, srv.TokenHandler(), url.Values{
		"grant_type": {"client_credentials"}, "scope": {"read:odds"},
	}, creds.ClientID, creds.ClientSecret)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	body := decodeJSON(t, rec)
	assert.Equal(t, "Bearer", body["token_type"])
	assert.Equal(t, "read:odds", body["scope"])
	assert.InDelta(t, 900, body["expires_in"], 1)

	// verify the JWT like a resource server would
	v, err := jwt.NewVerifier(
		jwt.WithKeys(signer.PublicKeys()...),
		jwt.WithIssuer("https://auth.example.com"),
		jwt.WithAudience("https://api.example.com"),
	)
	require.NoError(t, err)
	claims, err := jwt.Verify[ccClaims](context.Background(), v, body["access_token"].(string))
	require.NoError(t, err)
	assert.Equal(t, creds.ClientID, claims.Subject)
	assert.Equal(t, creds.ClientID, claims.ClientID)
	assert.Equal(t, "read:odds", claims.Scope)
	assert.NotEmpty(t, claims.ID, "jti present")
}

func TestClientCredentialsPostAuth(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	rec := postToken(t, srv.TokenHandler(), url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {creds.ClientID}, "client_secret": {creds.ClientSecret},
	}, "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestClientCredentialsOmittedScopeGrantsFullSet(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "a", "b")
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "a b", decodeJSON(t, rec)["scope"])
}

func TestClientCredentialsScopeSupersetRejected(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	rec := postToken(t, srv.TokenHandler(), url.Values{
		"grant_type": {"client_credentials"}, "scope": {"read admin"},
	}, creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_scope", decodeJSON(t, rec)["error"])
}

func TestTokenEndpointAuthFailures(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	h := srv.TokenHandler()

	for name, tc := range map[string]struct{ id, secret string }{
		"wrong secret":   {creds.ClientID, "osk_wrong"},
		"unknown client": {"client_nope", "osk_whatever"},
		"empty":          {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			rec := postToken(t, h, url.Values{"grant_type": {"client_credentials"}}, tc.id, tc.secret)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t, "invalid_client", decodeJSON(t, rec)["error"])
			assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "Basic")
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}
}

func TestTokenEndpointRevokedClient(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	require.NoError(t, srv.RevokeClient(context.Background(), creds.ClientID))
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "invalid_client", decodeJSON(t, rec)["error"])
}

func TestTokenEndpointGrantNotAllowed(t *testing.T) {
	srv, _ := newServer(t)
	// auth-code-only client trying client_credentials
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "app", Grants: []string{oauthserver.GrantAuthorizationCode},
		RedirectURIs: []string{"https://m1.example.com/cb"},
	})
	require.NoError(t, err)
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "unauthorized_client", decodeJSON(t, rec)["error"])
}

func TestTokenEndpointUnsupportedGrantAndMethod(t *testing.T) {
	srv, _ := newServer(t)
	creds := ccClient(t, srv, "read")
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"password"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "unsupported_grant_type", decodeJSON(t, rec)["error"])

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	rec2 := httptest.NewRecorder()
	srv.TokenHandler().ServeHTTP(rec2, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec2.Code)
}

func TestTokenClientTTLOverride(t *testing.T) {
	srv, _ := newServer(t)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials}, TokenTTL: 5 * time.Minute,
	})
	require.NoError(t, err)
	rec := postToken(t, srv.TokenHandler(), url.Values{"grant_type": {"client_credentials"}},
		creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.InDelta(t, 300, decodeJSON(t, rec)["expires_in"], 1)
}
