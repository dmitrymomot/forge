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

// fakeGitHub serves /user and /user/emails like api.github.com.
func fakeGitHub(t *testing.T, emailsStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer gho_token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 12345, "login": "octocat", "name": "Octo Cat",
			"avatar_url": "https://example.com/a.png", "email": "public@example.com",
		})
	})
	mux.HandleFunc("GET /user/emails", func(w http.ResponseWriter, r *http.Request) {
		if emailsStatus != http.StatusOK {
			w.WriteHeader(emailsStatus)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "old@example.com", "primary": false, "verified": true},
			{"email": "primary@example.com", "primary": true, "verified": true},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGitHubIdentityHook(t *testing.T) {
	srv := fakeGitHub(t, http.StatusOK)
	p := oauthclient.GitHub(oauthclient.ProviderConfig{ClientID: "c", ClientSecret: "s"})
	// Redirect the hook at the fake API (test-only export, see Step 3).
	p.Identity = oauthclient.GitHubIdentity(srv.URL)

	id, err := p.Identity(context.Background(), srv.Client(), oauthclient.TokenResponse{AccessToken: "gho_token"})
	require.NoError(t, err)
	assert.Equal(t, "12345", id.Subject)
	assert.Equal(t, "primary@example.com", id.Email)
	assert.True(t, id.EmailVerified)
	assert.Equal(t, "Octo Cat", id.Name)
	assert.Equal(t, "https://example.com/a.png", id.Picture)
	assert.Equal(t, "octocat", id.Raw["login"])
}

func TestGitHubIdentityHookEmailsForbiddenFallsBack(t *testing.T) {
	srv := fakeGitHub(t, http.StatusForbidden)
	hook := oauthclient.GitHubIdentity(srv.URL)
	id, err := hook(context.Background(), srv.Client(), oauthclient.TokenResponse{AccessToken: "gho_token"})
	require.NoError(t, err)
	assert.Equal(t, "public@example.com", id.Email)
	assert.False(t, id.EmailVerified)
}

func TestGitHubIdentityHookUserEndpointFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hook := oauthclient.GitHubIdentity(srv.URL)
	_, err := hook(context.Background(), srv.Client(), oauthclient.TokenResponse{AccessToken: "bad"})
	var perr *oauthclient.ProviderError
	require.ErrorAs(t, err, &perr)
	assert.Equal(t, "userinfo_failed", perr.Code) //nolint:nilaway // perr is guaranteed non-nil by require.ErrorAs above
}
