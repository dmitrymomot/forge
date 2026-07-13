package oauthserver_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/oauthserver"
)

func TestCreateClient(t *testing.T) {
	srv, store := newServer(t)
	creds, err := srv.CreateClient(context.Background(), oauthserver.CreateClientInput{
		Name:   "partner",
		Grants: []string{oauthserver.GrantClientCredentials},
		Scopes: []string{"read:odds"},
	})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(creds.ClientID, "client_"))
	assert.True(t, strings.HasPrefix(creds.ClientSecret, "osk_"))

	stored, err := store.Get(context.Background(), creds.ClientID)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.SecretHash)
	assert.NotContains(t, string(stored.SecretHash), creds.ClientSecret, "plaintext never stored")
	assert.False(t, stored.CreatedAt.IsZero())
}

func TestCreateClientValidation(t *testing.T) {
	srv, _ := newServer(t)
	ctx := context.Background()
	_, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{Grants: []string{"client_credentials"}})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "name required")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{Name: "x"})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "grants required")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{Name: "x", Grants: []string{"password"}})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "unknown grant")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{Name: "x", Grants: []string{oauthserver.GrantAuthorizationCode}})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "auth-code needs redirect URIs")
	_, err = srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "x", Grants: []string{oauthserver.GrantAuthorizationCode}, RedirectURIs: []string{"not a url"},
	})
	require.ErrorIs(t, err, oauthserver.ErrInvalidInput, "redirect URIs must be absolute URLs")
}

func TestRotateSecret(t *testing.T) {
	srv, store := newServer(t)
	ctx := context.Background()
	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(t, err)
	before, _ := store.Get(ctx, creds.ClientID)

	rotated, err := srv.RotateSecret(ctx, creds.ClientID)
	require.NoError(t, err)
	assert.Equal(t, creds.ClientID, rotated.ClientID)
	assert.NotEqual(t, creds.ClientSecret, rotated.ClientSecret)
	after, _ := store.Get(ctx, creds.ClientID)
	assert.NotEqual(t, before.SecretHash, after.SecretHash)
}

func TestRevokeClient(t *testing.T) {
	srv, store := newServer(t)
	ctx := context.Background()
	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(t, err)
	require.NoError(t, srv.RevokeClient(ctx, creds.ClientID))
	got, _ := store.Get(ctx, creds.ClientID)
	assert.True(t, got.Revoked())
	require.NoError(t, srv.RevokeClient(ctx, creds.ClientID), "revoke is idempotent")
	_, err = srv.RotateSecret(ctx, creds.ClientID)
	require.ErrorIs(t, err, oauthserver.ErrClientRevoked)
}

func TestManagementTenancyScoping(t *testing.T) {
	tenant := "t1"
	srv, _ := newServer(t, oauthserver.WithScope(func(ctx context.Context) (string, error) {
		if tenant == "" {
			return "", errors.New("no tenant in ctx")
		}
		return tenant, nil
	}))
	ctx := context.Background()

	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "m1", Grants: []string{oauthserver.GrantClientCredentials},
	})
	require.NoError(t, err)

	got, err := srv.GetClient(ctx, creds.ClientID)
	require.NoError(t, err)
	assert.Equal(t, "t1", got.TenantID, "create stamps the tenant")

	tenant = "t2" // same call, different tenant scope
	_, err = srv.GetClient(ctx, creds.ClientID)
	require.ErrorIs(t, err, oauthserver.ErrClientNotFound, "cross-tenant access is a not-found")
	require.ErrorIs(t, srv.RevokeClient(ctx, creds.ClientID), oauthserver.ErrClientNotFound)

	list, err := srv.ListClients(ctx)
	require.NoError(t, err)
	assert.Empty(t, list)

	tenant = "" // hook error → fail closed
	_, err = srv.ListClients(ctx)
	require.Error(t, err)
}

func TestNewValidation(t *testing.T) {
	_, err := oauthserver.New(nil, oauthserver.NewMemoryStore())
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
	_, err = oauthserver.New(testSigner(t), nil)
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
	_, err = oauthserver.New(testSigner(t), oauthserver.NewMemoryStore()) // no issuer
	require.ErrorIs(t, err, oauthserver.ErrInvalidConfig)
}

func TestClientTokenTTLOverrideStored(t *testing.T) {
	srv, store := newServer(t)
	ctx := context.Background()
	creds, err := srv.CreateClient(ctx, oauthserver.CreateClientInput{
		Name: "p", Grants: []string{oauthserver.GrantClientCredentials}, TokenTTL: 5 * time.Minute,
	})
	require.NoError(t, err)
	got, _ := store.Get(ctx, creds.ClientID)
	assert.Equal(t, 5*time.Minute, got.TokenTTL)
}
