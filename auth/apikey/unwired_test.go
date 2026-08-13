package apikey_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

func TestZeroManager_RejectedByEveryOperation(t *testing.T) {
	t.Parallel()
	zero := &apikey.Manager{}
	ctx := context.Background()
	someID := id.UUID{15: 1}

	t.Run("Create", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(zero.Create(ctx, apikey.CreateParams{Subject: "u1"}, discardKey))
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		_, err := zero.Get(ctx, someID, loadsKey(apikey.Key{}))
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("List", func(t *testing.T) {
		t.Parallel()
		_, err := zero.List(ctx, apikey.Filter{}, listsNothing)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Revoke", func(t *testing.T) {
		t.Parallel()
		err := zero.Revoke(ctx, someID, loadsKey(apikey.Key{}), discardStamp)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Rotate", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(zero.Rotate(ctx, someID, 0, loadsKey(apikey.Key{}), discardSwap))
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Verify", func(t *testing.T) {
		t.Parallel()
		_, err := zero.Verify(ctx, "key_whatever", loadsKeyByHash(apikey.Key{}), nil)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Verifier", func(t *testing.T) {
		t.Parallel()
		_, err := zero.Verifier(loadsKeyByHash(apikey.Key{}), nil)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})
}

// TestZeroManager_MintsNoPrefixlessKey pins the concrete damage the guard
// prevents: without it Create succeeded and returned a "_"-prefixed key
// that the same Manager then happily verified.
func TestZeroManager_MintsNoPrefixlessKey(t *testing.T) {
	t.Parallel()
	zero := &apikey.Manager{}

	k, plaintext, err := expose(zero.Create(context.Background(),
		apikey.CreateParams{Subject: "u1"}, discardKey))
	require.ErrorIs(t, err, apikey.ErrConfig)
	assert.Empty(t, plaintext)
	assert.Empty(t, k.Preview)
}

// TestNilManager_FailsAtWiringNotAtRequest pins that a caller who drops
// New's error learns about it while wiring, instead of panicking inside the
// first authenticated request.
func TestNilManager_FailsAtWiringNotAtRequest(t *testing.T) {
	t.Parallel()
	mgr, err := apikey.New(apikey.WithPrefix("SK-Live"))
	require.Error(t, err)
	require.Nil(t, mgr)

	verifier, err := mgr.Verifier(loadsKeyByHash(apikey.Key{}), nil)
	assert.ErrorIs(t, err, apikey.ErrConfig)
	assert.Nil(t, verifier)
}

func TestNilManager_RejectsOperations(t *testing.T) {
	t.Parallel()
	var mgr *apikey.Manager

	_, err := mgr.Verify(context.Background(), "key_whatever", loadsKeyByHash(apikey.Key{}), nil)
	assert.ErrorIs(t, err, apikey.ErrConfig)

	_, _, err = expose(mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"}, discardKey))
	assert.ErrorIs(t, err, apikey.ErrConfig)
}
