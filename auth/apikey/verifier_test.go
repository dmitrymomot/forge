package apikey_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/guard"
)

func TestNewVerifier_SatisfiesGuardSeam(t *testing.T) {
	t.Parallel()
	cfg, k, _ := verifiable(t)

	verifier, err := apikey.NewVerifier(cfg, loadsKeyByHash(k), nil)
	require.NoError(t, err)
	assert.Implements(t, (*guard.Verifier)(nil), verifier)
}

func TestNewVerifier_ResolvesTheSameIdentityAsVerify(t *testing.T) {
	t.Parallel()
	cfg, k, plaintext := verifiable(t)

	verifier, err := apikey.NewVerifier(cfg, loadsKeyByHash(k), nil)
	require.NoError(t, err)

	curried, err := verifier.Verify(context.Background(), plaintext)
	require.NoError(t, err)
	direct, err := apikey.Verify(context.Background(), cfg, plaintext, loadsKeyByHash(k), nil)
	require.NoError(t, err)

	assert.Equal(t, direct, curried)
}

func TestNewVerifier_PropagatesRejections(t *testing.T) {
	t.Parallel()
	cfg, k, plaintext := verifiable(t)

	verifier, err := apikey.NewVerifier(cfg, loadsKeyByHash(k), nil)
	require.NoError(t, err)

	_, err = verifier.Verify(context.Background(), tamper(plaintext))
	assert.ErrorIs(t, err, apikey.ErrMalformedKey)
}
