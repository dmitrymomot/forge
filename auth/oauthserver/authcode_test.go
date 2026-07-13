package oauthserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthserver"
)

// obtainCode drives the authorize endpoint and returns the issued code.
func obtainCode(t *testing.T, srv *oauthserver.Server, clientID string) string {
	t.Helper()
	h, err := srv.AuthorizeHandler()
	require.NoError(t, err)
	rec := getAuthorize(t, h, authorizeQuery(clientID))
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

// redeemForm builds a valid auth-code redemption form.
func redeemForm(code string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://mirror1.example.com/cb"},
		"code_verifier": {"verifier-123"},
	}
}

type idClaims struct {
	jwt.Claims
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

func TestAuthCodeRedemptionHappyPath(t *testing.T) {
	signer := testSigner(t)
	store := oauthserver.NewMemoryStore()
	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = "https://auth.example.com"
	srv, err := oauthserver.New(signer, store,
		oauthserver.WithConfig(cfg),
		oauthserver.WithCodeStore(cacheStore(t)),
		oauthserver.WithCodeKeyset(testKeyset(t)),
		oauthserver.WithAuthenticator(staticUser("user-1")),
		oauthserver.WithUserClaims(func(ctx context.Context, subject string) (map[string]any, error) {
			return map[string]any{"email": "u1@example.com", "sub": "OVERRIDE-IGNORED"}, nil
		}),
	)
	require.NoError(t, err)
	creds := acClient(t, srv)

	code := obtainCode(t, srv, creds.ClientID)
	rec := postToken(t, srv.TokenHandler(), redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := decodeJSON(t, rec)
	assert.Equal(t, "profile", body["scope"])
	require.NotEmpty(t, body["id_token"])
	require.NotEmpty(t, body["access_token"])

	// access token: sub = user, client_id = client, verified via JWKS keys
	av, err := jwt.NewVerifier(jwt.WithKeys(signer.PublicKeys()...), jwt.WithIssuer("https://auth.example.com"))
	require.NoError(t, err)
	access, err := jwt.Verify[ccClaims](context.Background(), av, body["access_token"].(string))
	require.NoError(t, err)
	assert.Equal(t, "user-1", access.Subject)
	assert.Equal(t, creds.ClientID, access.ClientID)

	// id_token: aud = client_id, nonce echoed, user claims merged, sub protected
	iv, err := jwt.NewVerifier(jwt.WithKeys(signer.PublicKeys()...),
		jwt.WithIssuer("https://auth.example.com"), jwt.WithAudience(creds.ClientID))
	require.NoError(t, err)
	idt, err := jwt.Verify[idClaims](context.Background(), iv, body["id_token"].(string))
	require.NoError(t, err)
	assert.Equal(t, "user-1", idt.Subject, "reserved sub claim survives the hook")
	assert.Equal(t, "n-1", idt.Nonce)
	assert.Equal(t, "u1@example.com", idt.Email)
}

func TestAuthCodeReplayRejected(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	h := srv.TokenHandler()

	rec := postToken(t, h, redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusOK, rec.Code)

	rec2 := postToken(t, h, redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec2.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec2)["error"])
}

func TestAuthCodeWrongVerifier(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	form := redeemForm(code)
	form.Set("code_verifier", "wrong-verifier")
	rec := postToken(t, srv.TokenHandler(), form, creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec)["error"])
}

func TestAuthCodeWrongRedirectURI(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	form := redeemForm(code)
	form.Set("redirect_uri", "https://mirror1.example.com/cb2")
	rec := postToken(t, srv.TokenHandler(), form, creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec)["error"])
}

func TestAuthCodeOtherClientCannotRedeem(t *testing.T) {
	srv := acServer(t, "user-1", true)
	creds := acClient(t, srv)
	other, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "other", Grants: []string{oauthserver.GrantAuthorizationCode},
		RedirectURIs: []string{"https://mirror1.example.com/cb"},
	})
	require.NoError(t, err)
	code := obtainCode(t, srv, creds.ClientID)
	rec := postToken(t, srv.TokenHandler(), redeemForm(code), other.ClientID, other.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_grant", decodeJSON(t, rec)["error"])
}

func TestAuthCodeGrantWithoutCodeStore(t *testing.T) {
	srv, _ := newServer(t) // no code store / keyset
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name: "app", Grants: []string{oauthserver.GrantAuthorizationCode},
		RedirectURIs: []string{"https://m/cb"},
	})
	require.NoError(t, err)
	rec := postToken(t, srv.TokenHandler(), redeemForm("some-code"), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "unsupported_grant_type", decodeJSON(t, rec)["error"])
}

func TestAuthCodeUserClaimsHookErrorFailsClosed(t *testing.T) {
	srv := acServer(t, "user-1", true,
		oauthserver.WithUserClaims(func(ctx context.Context, subject string) (map[string]any, error) {
			return nil, errors.New("directory down")
		}))
	creds := acClient(t, srv)
	code := obtainCode(t, srv, creds.ClientID)
	rec := postToken(t, srv.TokenHandler(), redeemForm(code), creds.ClientID, creds.ClientSecret)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "server_error", decodeJSON(t, rec)["error"])
}
