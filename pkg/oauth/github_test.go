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

var _ oauth.Provider = (*oauth.GitHubProvider)(nil)

func TestNewGitHubProvider(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
		})
		require.NoError(t, err)
		require.NotNil(t, p)
	})

	t.Run("missing client ID", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
			ClientSecret: "test-secret",
		})
		require.ErrorIs(t, err, oauth.ErrMissingClientID)
		require.Nil(t, p)
	})

	t.Run("missing client secret", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
			ClientID: "test-id",
		})
		require.ErrorIs(t, err, oauth.ErrMissingClientSecret)
		require.Nil(t, p)
	})

	t.Run("default scopes applied", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
		})
		require.NoError(t, err)

		u := p.AuthCodeURL("state")
		// Scopes are URL-encoded in the query string (: becomes %3A)
		require.Contains(t, u, "scope=")
		require.Contains(t, u, "user")
		require.Contains(t, u, "email")
	})

	t.Run("custom scopes", func(t *testing.T) {
		t.Parallel()
		p, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
			ClientID:     "test-id",
			ClientSecret: "test-secret",
			Scopes:       []string{"repo"},
		})
		require.NoError(t, err)

		url := p.AuthCodeURL("state")
		require.Contains(t, url, "repo")
		require.NotContains(t, url, "read%3Auser")
	})
}

func TestGitHubProvider_Name(t *testing.T) {
	t.Parallel()
	p, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
	})
	require.NoError(t, err)
	require.Equal(t, "github", p.Name())
}

func TestGitHubProvider_AuthCodeURL(t *testing.T) {
	t.Parallel()

	p, err := oauth.NewGitHubProvider(oauth.GitHubConfig{
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

func TestGitHubDefaultScopes(t *testing.T) {
	t.Parallel()
	scopes := oauth.GitHubDefaultScopes()
	require.Len(t, scopes, 2)
	require.Contains(t, scopes, "read:user")
	require.Contains(t, scopes, "user:email")
}

func TestGitHubProvider_Exchange(t *testing.T) {
	t.Parallel()

	t.Run("successful exchange", func(t *testing.T) {
		t.Parallel()

		transport := &githubRewriteTransport{
			base: http.DefaultTransport,
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "gh-test-token",
					"token_type":   "Bearer",
					"scope":        "read:user,user:email",
				})
			}),
		}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token, err := p.Exchange(context.Background(), "test-code", "")
		require.NoError(t, err)
		require.Equal(t, "gh-test-token", token.AccessToken)
	})

	t.Run("invalid code", func(t *testing.T) {
		t.Parallel()

		transport := &githubRewriteTransport{
			base: http.DefaultTransport,
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "bad_verification_code",
					"error_description": "The code passed is incorrect or expired.",
				})
			}),
		}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
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

func TestGitHubProvider_FetchUserInfo(t *testing.T) {
	t.Parallel()

	t.Run("primary verified email", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         42,
				"name":       "Octocat",
				"avatar_url": "https://example.com/octocat.png",
			})
		})
		mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "secondary@example.com", "primary": false, "verified": true},
				{"email": "primary@example.com", "primary": true, "verified": true},
			})
		})

		transport := &githubRewriteTransport{base: http.DefaultTransport, handler: mux}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token := &oauth2.Token{AccessToken: "test-token"}
		user, err := p.FetchUserInfo(context.Background(), token)
		require.NoError(t, err)
		require.Equal(t, "42", user.ID)
		require.Equal(t, "primary@example.com", user.Email)
		require.Equal(t, "Octocat", user.Name)
		require.Equal(t, "https://example.com/octocat.png", user.Picture)
	})

	t.Run("fallback verified email", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         42,
				"name":       "Octocat",
				"avatar_url": "https://example.com/octocat.png",
			})
		})
		mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "unverified@example.com", "primary": true, "verified": false},
				{"email": "verified@example.com", "primary": false, "verified": true},
			})
		})

		transport := &githubRewriteTransport{base: http.DefaultTransport, handler: mux}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
				ClientID:     "test-id",
				ClientSecret: "test-secret",
			},
			oauth.WithHTTPClient(&http.Client{Transport: transport}),
		)
		require.NoError(t, err)

		token := &oauth2.Token{AccessToken: "test-token"}
		user, err := p.FetchUserInfo(context.Background(), token)
		require.NoError(t, err)
		require.Equal(t, "verified@example.com", user.Email)
	})

	t.Run("no verified email", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   42,
				"name": "Octocat",
			})
		})
		mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"email": "nope@example.com", "primary": true, "verified": false},
			})
		})

		transport := &githubRewriteTransport{base: http.DefaultTransport, handler: mux}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
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

	t.Run("user endpoint error", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		})

		transport := &githubRewriteTransport{base: http.DefaultTransport, handler: mux}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
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

	t.Run("emails endpoint error", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   42,
				"name": "Octocat",
			})
		})
		mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})

		transport := &githubRewriteTransport{base: http.DefaultTransport, handler: mux}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
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

	t.Run("bad JSON from user endpoint", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not-json"))
		})

		transport := &githubRewriteTransport{base: http.DefaultTransport, handler: mux}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
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

	t.Run("bad JSON from emails endpoint", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   42,
				"name": "Octocat",
			})
		})
		mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not-json"))
		})

		transport := &githubRewriteTransport{base: http.DefaultTransport, handler: mux}

		p, err := oauth.NewGitHubProvider(
			oauth.GitHubConfig{
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

// githubRewriteTransport intercepts requests to GitHub API endpoints and routes them
// to a local handler instead.
type githubRewriteTransport struct {
	base    http.RoundTripper
	handler http.Handler
}

func (t *githubRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Host, "github.com") {
		recorder := httptest.NewRecorder()
		t.handler.ServeHTTP(recorder, req)
		return recorder.Result(), nil
	}
	return t.base.RoundTrip(req)
}
