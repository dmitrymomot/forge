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
	mgr, k, _ := verifiable(t)

	verifier, err := mgr.Verifier(loadsKeyByHash(k), nil)
	require.NoError(t, err)
	assert.Implements(t, (*guard.Verifier)(nil), verifier)
}

func TestNewVerifier_ResolvesTheSameIdentityAsVerify(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	verifier, err := mgr.Verifier(loadsKeyByHash(k), nil)
	require.NoError(t, err)

	curried, err := verifier.Verify(context.Background(), plaintext)
	require.NoError(t, err)
	direct, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	require.NoError(t, err)

	assert.Equal(t, direct, curried)
}

func TestNewVerifier_PropagatesRejections(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	verifier, err := mgr.Verifier(loadsKeyByHash(k), nil)
	require.NoError(t, err)

	_, err = verifier.Verify(context.Background(), tamper(plaintext))
	assert.ErrorIs(t, err, apikey.ErrMalformedKey)
}
