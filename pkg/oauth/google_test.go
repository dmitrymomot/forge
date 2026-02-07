package oauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/dmitrymomot/forge/pkg/oauth"
)

var _ oauth.Provider = (*oauth.GoogleProvider)(nil)

func TestNewGoogleProvider(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
		})
		require.NoError(t, err)
		require.NotNil(t, p)
	})

	t.Run("missing client ID", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
			ClientSecret: "test-secret",
		})
		require.ErrorIs(t, err, oauth.ErrMissingClientID)
		require.Nil(t, p)
	})

	t.Run("missing client secret", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
			ClientID: "test-id",
		})
		require.ErrorIs(t, err, oauth.ErrMissingClientSecret)
		require.Nil(t, p)
	})

	t.Run("default scopes applied", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
		})
		require.NoError(t, err)

		u := p.AuthCodeURL("state")
		// Scopes are URL-encoded in the query string
		require.Contains(t, u, "scope=")
		require.Contains(t, u, "userinfo.email")
		require.Contains(t, u, "userinfo.profile")
	})

	t.Run("custom scopes", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			Scopes:       []string{"openid"},
		})
		require.NoError(t, err)

		url := p.AuthCodeURL("state")
		require.Contains(t, url, "openid")
		require.NotContains(t, url, "userinfo.email")
	})
}

func TestGoogleProvider_Name(t *testing.T) {
	t.Parallel()
	p, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
	})
	require.NoError(t, err)
	require.Equal(t, "google", p.Name())
}

func TestGoogleProvider_AuthCodeURL(t *testing.T) {
	t.Parallel()

	p, err := oauth.NewGoogleProvider(oauth.GoogleConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
		RedirectURL:  "https://example.com/callback",
	})
	require.NoError(t, err)

	t.Run("includes state", func(t *testing.T) {
		t.Parallel()
		url := p.AuthCodeURL("test-state")
		require.Contains(t, url, "state=test-state")
	})

	t.Run("includes redirect URI", func(t *testing.T) {
		t.Parallel()
		url := p.AuthCodeURL("state")
		require.Contains(t, url, "redirect_uri=")
		require.Contains(t, url, "example.com")
	})

	t.Run("includes scopes", func(t *testing.T) {
		t.Parallel()
		url := p.AuthCodeURL("state")
		require.Contains(t, url, "scope=")
	})
}

func TestGoogleDefaultScopes(t *testing.T) {
	t.Parallel()
	scopes := oauth.GoogleDefaultScopes()
	require.Len(t, scopes, 2)
	require.Contains(t, scopes, "https://www.googleapis.com/auth/userinfo.email")
	require.Contains(t, scopes, "https://www.googleapis.com/auth/userinfo.profile")
}

func TestGoogleProvider_Exchange(t *testing.T) {
	t.Parallel()

	t.Run("successful exchange", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "test-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		})

		transport := &googleRewriteTransport{base: http.DefaultTransport, handler: handler}

		p, err := oauth.NewGoogleProvider(
			oauth.GoogleConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token, err := p.Exchange(context.Background(), "test-code", "")
		require.NoError(t, err)
		require.Equal(t, "test-access-token", token.AccessToken)
	})

	t.Run("custom redirect URI", func(t *testing.T) {
		t.Parallel()

		var receivedRedirectURI string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedRedirectURI = r.FormValue("redirect_uri")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "test-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		})

		transport := &googleRewriteTransport{base: http.DefaultTransport, handler: handler}

		p, err := oauth.NewGoogleProvider(
			oauth.GoogleConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
				RedirectURL:  "https://example.com/original",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		_, err = p.Exchange(context.Background(), "test-code", "https://example.com/override")
		require.NoError(t, err)
		require.Equal(t, "https://example.com/override", receivedRedirectURI)
	})

	t.Run("invalid code", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_grant",
				"error_description": "Bad Request",
			})
		})

		transport := &googleRewriteTransport{base: http.DefaultTransport, handler: handler}

		p, err := oauth.NewGoogleProvider(
			oauth.GoogleConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		_, err = p.Exchange(context.Background(), "bad-code", "")
		require.Error(t, err)
	})
}

func TestGoogleProvider_FetchUserInfo(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "12345",
				"email":          "user@example.com",
				"name":           "Test User",
				"picture":        "https://example.com/photo.jpg",
				"verified_email": true,
			})
		})

		transport := &googleRewriteTransport{base: http.DefaultTransport, handler: handler}

		p, err := oauth.NewGoogleProvider(
			oauth.GoogleConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token := &oauth2.Token{AccessToken: "test-token"}
		user, err := p.FetchUserInfo(context.Background(), token)
		require.NoError(t, err)
		require.Equal(t, "12345", user.ID)
		require.Equal(t, "user@example.com", user.Email)
		require.Equal(t, "Test User", user.Name)
		require.Equal(t, "https://example.com/photo.jpg", user.Picture)
	})

	t.Run("unverified email", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "12345",
				"email":          "user@example.com",
				"verified_email": false,
			})
		})

		transport := &googleRewriteTransport{base: http.DefaultTransport, handler: handler}

		p, err := oauth.NewGoogleProvider(
			oauth.GoogleConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token := &oauth2.Token{AccessToken: "test-token"}
		user, err := p.FetchUserInfo(context.Background(), token)
		require.ErrorIs(t, err, oauth.ErrEmailNotVerified)
		require.Nil(t, user)
	})

	t.Run("non-OK status", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("forbidden"))
		})

		transport := &googleRewriteTransport{base: http.DefaultTransport, handler: handler}

		p, err := oauth.NewGoogleProvider(
			oauth.GoogleConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token := &oauth2.Token{AccessToken: "test-token"}
		user, err := p.FetchUserInfo(context.Background(), token)
		require.ErrorIs(t, err, oauth.ErrRequestFailed)
		require.Nil(t, user)
	})

	t.Run("bad JSON", func(t *testing.T) {
		t.Parallel()

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not-json"))
		})

		transport := &googleRewriteTransport{base: http.DefaultTransport, handler: handler}

		p, err := oauth.NewGoogleProvider(
			oauth.GoogleConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token := &oauth2.Token{AccessToken: "test-token"}
		user, err := p.FetchUserInfo(context.Background(), token)
		require.ErrorIs(t, err, oauth.ErrDecodeFailed)
		require.Nil(t, user)
	})
}

// googleRewriteTransport intercepts requests to Google endpoints and routes them
// to a local handler instead.
type googleRewriteTransport struct {
	base    http.RoundTripper
	handler http.Handler
}

func (t *googleRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "google") || strings.Contains(req.URL.Host, "googleapis") {
		recorder := httptest.NewRecorder()
		t.handler.ServeHTTP(recorder, req)
		return recorder.Result(), nil
	}
	return t.base.RoundTrip(req)
}
