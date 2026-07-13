package oauthclient_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthclient"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// idpSigner builds an Ed25519 jwt.Signer for minting fake id_tokens.
func idpSigner(t *testing.T) *jwt.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	ks, err := keyset.New(keyset.WithPrimary(1, der))
	require.NoError(t, err)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	require.NoError(t, err)
	return s
}

// fakeOIDC is an httptest IdP: /token returns tokenResp (with a freshly
// signed id_token when mintIDToken is set), /jwks serves the signer's keys.
type fakeOIDC struct {
	Server    *httptest.Server
	Signer    *jwt.Signer
	TokenForm url.Values // captured form of the last /token POST
	// IDTokenClaims lets a test override claims minted into the id_token.
	// Keys iss/aud/exp are filled with valid values unless already set.
	IDTokenClaims map[string]any
	TokenStatus   int
	TokenBody     map[string]any // non-nil overrides the default token response
}

func newFakeOIDC(t *testing.T) *fakeOIDC {
	t.Helper()
	f := &fakeOIDC{Signer: idpSigner(t), TokenStatus: http.StatusOK, IDTokenClaims: map[string]any{}}
	mux := http.NewServeMux()
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	mux.Handle("GET /jwks", f.Signer.JWKS())
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		f.TokenForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		if f.TokenStatus != http.StatusOK {
			w.WriteHeader(f.TokenStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "bad code"})
			return
		}
		body := f.TokenBody
		if body == nil {
			claims := map[string]any{
				"iss": f.Server.URL, "aud": "cid", "sub": "user-1",
				"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
				"email": "u@example.com", "email_verified": true,
				"name": "User One", "picture": "https://img.example.com/u.png",
			}
			maps.Copy(claims, f.IDTokenClaims)
			idt, err := f.Signer.Sign(claims)
			require.NoError(t, err)
			body = map[string]any{
				"access_token": "at-123", "token_type": "Bearer",
				"expires_in": 3600, "id_token": idt, "scope": "openid email",
			}
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	return f
}

// provider returns a Provider pointed at the fake IdP.
func (f *fakeOIDC) provider() oauthclient.Provider {
	return oauthclient.Provider{
		ClientID: "cid", ClientSecret: "sec",
		AuthURL:  f.Server.URL + "/authorize",
		TokenURL: f.Server.URL + "/token",
		JWKSURL:  f.Server.URL + "/jwks",
		Issuer:   f.Server.URL,
		Scopes:   []string{"openid", "email"},
	}
}

// newClient builds an oauthclient against the fake IdP.
func (f *fakeOIDC) newClient(t *testing.T, opts ...oauthclient.Option) *oauthclient.Client {
	t.Helper()
	base := []oauthclient.Option{
		oauthclient.WithRedirectURL("https://app.example.com/cb"),
		oauthclient.WithProvider("idp", f.provider()),
		oauthclient.WithHTTPClient(f.Server.Client()),
	}
	c, err := oauthclient.New(testKeyset(t), append(base, opts...)...)
	require.NoError(t, err)
	return c
}

// startFlow runs AuthURL and returns the flow plus the authorize-URL query
// (which carries state/nonce the callback must echo).
func startFlow(t *testing.T, c *oauthclient.Client, opts ...oauthclient.BeginOption) (*oauthclient.Flow, url.Values) {
	t.Helper()
	flow, err := c.AuthURL(t.Context(), "idp", opts...)
	require.NoError(t, err)
	u, err := url.Parse(flow.URL)
	require.NoError(t, err)
	return flow, u.Query()
}

// callbackQuery fabricates the provider redirect query for a started flow.
func callbackQuery(authQ url.Values, code string) url.Values {
	return url.Values{"code": {code}, "state": {authQ.Get("state")}}
}

// withNonce makes the fake mint the nonce the started flow expects: the
// authorize query carries it, and f.IDTokenClaims overrides flow into the
// minted id_token. Tests that skip it exercise the nonce-mismatch path.
func withNonce(f *fakeOIDC, authQ url.Values) {
	f.IDTokenClaims["nonce"] = authQ.Get("nonce")
}
