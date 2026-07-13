package oauthserver_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/jwt"
	"github.com/dmitrymomot/forge/auth/oauthserver"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/resilience/cache"
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

// staticUser is an Authenticator that always returns subject.
func staticUser(subject string) func(http.ResponseWriter, *http.Request) (string, bool) {
	return func(w http.ResponseWriter, r *http.Request) (string, bool) { return subject, true }
}

// cacheStore returns a memory cache.Store cleaned up with the test.
func cacheStore(tb testing.TB) cache.Store {
	tb.Helper()
	s := cache.NewMemoryStore()
	tb.Cleanup(func() { _ = s.Close() })
	return s
}
