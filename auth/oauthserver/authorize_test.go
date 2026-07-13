package oauthserver_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// acServer builds a Server wired for the auth-code flow with a static
// logged-in subject.
func acServer(t *testing.T, subject string, ok bool, opts ...oauthserver.Option) *oauthserver.Server {
	t.Helper()
	store := cache.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	base := []oauthserver.Option{
		oauthserver.WithCodeStore(store),
		oauthserver.WithCodeKeyset(testKeyset(t)),
		oauthserver.WithAuthenticator(func(w http.ResponseWriter, r *http.Request) (string, bool) {
			if !ok {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
				return "", false
			}
			return subject, true
		}),
	}
	srv, _ := newServer(t, append(base, opts...)...)
	return srv
}

// acClient registers an auth-code client.
func acClient(t *testing.T, srv *oauthserver.Server, redirects ...string) *oauthserver.ClientCredentials {
	t.Helper()
	if len(redirects) == 0 {
		redirects = []string{"https://mirror1.example.com/cb"}
	}
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "mirror", Grants: []string{oauthserver.GrantAuthorizationCode},
		Scopes: []string{"profile", "email"}, RedirectURIs: redirects,
	})
	require.NoError(t, err)
	return creds
}

func s256(verifier string) string {
	return base64.RawURLEncoding.EncodeToString(digest.SHA256([]byte(verifier)))
}

// authorizeQuery builds a valid authorize request query.
func authorizeQuery(clientID string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"https://mirror1.example.com/cb"},
		"scope":                 {"profile"},
		"state":                 {"st-1"},
		"nonce":                 {"n-1"},
		"code_challenge":        {s256("verifier-123")},
		"code_challenge_method": {"S256"},
	}
}

func getAuthorize(t *testing.T, h http.Handler, q url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthorizeHappyPath(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	rec := getAuthorize(t, h, authorizeQuery(creds.ClientID))
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "mirror1.example.com", loc.Host)
	assert.NotEmpty(t, loc.Query().Get("code"))
	assert.Equal(t, "st-1", loc.Query().Get("state"))
	assert.Empty(t, loc.Query().Get("error"))
}

func TestAuthorizeInvalidClientOrRedirectIsLocal400(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	q := authorizeQuery(creds.ClientID)
	q.Set("redirect_uri", "https://evil.example.com/cb")
	rec := getAuthorize(t, h, q)
	require.Equal(t, http.StatusBadRequest, rec.Code, "unregistered redirect_uri never gets a redirect")

	q2 := authorizeQuery("client_unknown")
	rec2 := getAuthorize(t, h, q2)
	require.Equal(t, http.StatusBadRequest, rec2.Code)
}

func TestAuthorizeProtocolErrorsRedirectBack(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	for name, mutate := range map[string]struct {
		key, val, wantErr string
	}{
		"bad response_type": {"response_type", "token", "unsupported_response_type"},
		"missing pkce":      {"code_challenge", "", "invalid_request"},
		"plain pkce":        {"code_challenge_method", "plain", "invalid_request"},
		"scope superset":    {"scope", "profile admin", "invalid_scope"},
	} {
		t.Run(name, func(t *testing.T) {
			q := authorizeQuery(creds.ClientID)
			if mutate.val == "" {
				q.Del(mutate.key)
			} else {
				q.Set(mutate.key, mutate.val)
			}
			rec := getAuthorize(t, h, q)
			require.Equal(t, http.StatusFound, rec.Code)
			loc, _ := url.Parse(rec.Header().Get("Location"))
			assert.Equal(t, mutate.wantErr, loc.Query().Get("error"))
			assert.Equal(t, "st-1", loc.Query().Get("state"))
			assert.Empty(t, loc.Query().Get("code"))
		})
	}
}

func TestAuthorizeUnauthenticatedDelegatesToSeam(t *testing.T) {
	srv := acServer(t, "", false)
	creds := acClient(t, srv)
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)

	rec := getAuthorize(t, h, authorizeQuery(creds.ClientID))
	require.Equal(t, http.StatusSeeOther, rec.Code, "authenticator wrote its login redirect")
	assert.Contains(t, rec.Header().Get("Location"), "/login?next=")
}

func TestAuthorizeHandlerRequiresSeams(t *testing.T) {
	srv, _ := newServer(t) // no authenticator/code store/keyset
	_, err := srv.AuthorizeHandler()
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
}

func TestAuthorizeRevokedClient(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	require.NoError(t, srv.RevokeClient(context.Background(), creds.ClientID))
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)
	rec := getAuthorize(t, h, authorizeQuery(creds.ClientID))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
