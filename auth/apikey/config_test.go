package apikey_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

func TestNewConfig_RejectsEmptyPrefix(t *testing.T) {
	t.Parallel()
	_, err := apikey.NewConfig(apikey.WithPrefix(""))
	assert.ErrorIs(t, err, apikey.ErrConfig)
}

func TestNewConfig_RejectsPrefixOutsideAlphabet(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"SK-Live", "sk live", "sk.live", "sk/live"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			_, err := apikey.NewConfig(apikey.WithPrefix(prefix))
			assert.ErrorIs(t, err, apikey.ErrConfig)
		})
	}
}

func TestNewConfig_DefaultsToKeyPrefix(t *testing.T) {
	t.Parallel()
	_, plaintext := issueKey(t, mustConfig(t), apikey.CreateParams{Subject: "u1"})
	assert.True(t, strings.HasPrefix(plaintext, "key_"))
}

// TestZeroConfig_RejectedByEveryOperation pins the guard that replaces the
// old constructor panic: a Config that did not come from NewConfig has an
// empty prefix, and no operation proceeds on one.
func TestZeroConfig_RejectedByEveryOperation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var zero apikey.Config
	someID := id.UUID{15: 1}

	t.Run("Create", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(apikey.Create(ctx, zero, apikey.CreateParams{Subject: "u1"}, discardKey))
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.Get(ctx, zero, someID, loadsKey(apikey.Key{}))
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("List", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.List(ctx, zero, apikey.Filter{}, listsNothing)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Revoke", func(t *testing.T) {
		t.Parallel()
		err := apikey.Revoke(ctx, zero, someID, loadsKey(apikey.Key{}), discardStamp)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("Verify", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.Verify(ctx, zero, "key_whatever", loadsKeyByHash(apikey.Key{}), nil)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})

	t.Run("NewVerifier", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.NewVerifier(zero, loadsKeyByHash(apikey.Key{}), nil)
		assert.ErrorIs(t, err, apikey.ErrConfig)
	})
}

// TestNilEffect_RejectedByEveryOperation pins that a missing effect is an
// error, never a nil dereference. TouchFunc is the one exception and has
// its own test.
func TestNilEffect_RejectedByEveryOperation(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	ctx := context.Background()
	someID := id.UUID{15: 1}

	t.Run("Create save", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, nil))
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Get load", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.Get(ctx, cfg, someID, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("List list", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.List(ctx, cfg, apikey.Filter{}, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Revoke revoke", func(t *testing.T) {
		t.Parallel()
		err := apikey.Revoke(ctx, cfg, someID, loadsKey(apikey.Key{}), nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Rotate swap", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(apikey.Rotate(ctx, cfg, someID, 0, loadsKey(apikey.Key{}), nil))
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Verify load", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.Verify(ctx, cfg, "key_whatever", nil, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("NewVerifier load", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.NewVerifier(cfg, nil, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})
}

// TestNilEffect_CheckedBeforeAnyWork pins the order: a nil effect is
// reported without minting a key or resolving a tenant scope.
func TestNilEffect_CheckedBeforeAnyWork(t *testing.T) {
	t.Parallel()
	scopeCalled := false
	cfg := mustConfig(t, apikey.WithScope(func(context.Context) (string, error) {
		scopeCalled = true
		return "t1", nil
	}))

	_, _, err := expose(apikey.Create(context.Background(), cfg, apikey.CreateParams{Subject: "u1"}, nil))
	require.ErrorIs(t, err, apikey.ErrNilEffect)
	assert.False(t, scopeCalled)
}
