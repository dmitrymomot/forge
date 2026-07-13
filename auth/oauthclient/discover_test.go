package oauthclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthclient"
)

func fakeIssuer(t *testing.T, mutate func(doc map[string]string, issuer string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]string{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/jwks",
		}
		if mutate != nil {
			mutate(doc, srv.URL)
		}
		_ = json.NewEncoder(w).Encode(doc)
	})
	return srv
}

func TestDiscover(t *testing.T) {
	srv := fakeIssuer(t, nil)
	p, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "cid", ClientSecret: "sec"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/authorize", p.AuthURL)
	assert.Equal(t, srv.URL+"/token", p.TokenURL)
	assert.Equal(t, srv.URL+"/jwks", p.JWKSURL)
	assert.Equal(t, srv.URL, p.Issuer)
	assert.Equal(t, []string{"openid", "email", "profile"}, p.Scopes)
	assert.Nil(t, p.Identity)
}

func TestDiscoverTrailingSlashAndScopeOverride(t *testing.T) {
	srv := fakeIssuer(t, nil)
	p, err := oauthclient.Discover(context.Background(), srv.URL+"/",
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s", Scopes: []string{"openid", "groups"}},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.NoError(t, err)
	assert.Equal(t, []string{"openid", "groups"}, p.Scopes)
}

func TestDiscoverIssuerMismatch(t *testing.T) {
	srv := fakeIssuer(t, func(doc map[string]string, _ string) { doc["issuer"] = "https://evil.example.com" })
	_, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.ErrorIs(t, err, oauthclient.ErrDiscovery)
}

func TestDiscoverHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	_, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.ErrorIs(t, err, oauthclient.ErrDiscovery)
}

func TestDiscoverIncompleteDocument(t *testing.T) {
	srv := fakeIssuer(t, func(doc map[string]string, _ string) { delete(doc, "token_endpoint") })
	_, err := oauthclient.Discover(context.Background(), srv.URL,
		oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"},
		oauthclient.WithDiscoverClient(srv.Client()))
	require.ErrorIs(t, err, oauthclient.ErrDiscovery)
}
