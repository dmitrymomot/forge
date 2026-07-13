package oauthserver_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

// testSigner builds an Ed25519 jwt.Signer.
func testSigner(tb testing.TB) *jwt.Signer {
	tb.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(tb, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(tb, err)
	ks, err := keyset.New(keyset.WithPrimary(1, der))
	require.NoError(tb, err)
	s, err := jwt.NewSigner(jwt.WithKeyset(ks))
	require.NoError(tb, err)
	return s
}

// testKeyset builds an HMAC keyset for sealing auth codes.
//
//nolint:unused // consumed in Task 10 (WithCodeKeyset for the authorize endpoint)
func testKeyset(tb testing.TB) *keyset.Keyset {
	tb.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("0123456789abcdef0123456789abcdef")))
	require.NoError(tb, err)
	return ks
}

// newServer builds a Server over a fresh memory store with a valid Config.
func newServer(tb testing.TB, opts ...oauthserver.Option) (*oauthserver.Server, oauthserver.Store) {
	tb.Helper()
	store := oauthserver.NewMemoryStore()
	cfg := oauthserver.DefaultConfig()
	cfg.Issuer = "https://auth.example.com"
	cfg.Audience = "https://api.example.com"
	base := []oauthserver.Option{oauthserver.WithConfig(cfg)}
	srv, err := oauthserver.New(testSigner(tb), store, append(base, opts...)...)
	require.NoError(tb, err)
	return srv, store
}
