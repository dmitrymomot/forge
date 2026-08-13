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

func TestNew_RejectsEmptyPrefix(t *testing.T) {
	t.Parallel()
	_, err := apikey.New(apikey.WithPrefix(""))
	assert.ErrorIs(t, err, apikey.ErrConfig)
}

func TestNew_RejectsPrefixOutsideAlphabet(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"SK-Live", "sk live", "sk.live", "sk/live"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			_, err := apikey.New(apikey.WithPrefix(prefix))
			assert.ErrorIs(t, err, apikey.ErrConfig)
		})
	}
}

// TestNew_ReportsEveryBadOption pins the accumulating contract: one call
// names every invalid value instead of stopping at the first.
func TestNew_ReportsEveryBadOption(t *testing.T) {
	t.Parallel()
	_, err := apikey.New(apikey.WithPrefix("SK-Live"), apikey.WithPrefix("sk live"))
	require.ErrorIs(t, err, apikey.ErrConfig)
	assert.Contains(t, err.Error(), "SK-Live")
	assert.Contains(t, err.Error(), "sk live")
}

func TestNew_RejectedOptionYieldsNoManager(t *testing.T) {
	t.Parallel()
	mgr, err := apikey.New(apikey.WithPrefix("SK-Live"))
	require.Error(t, err)
	assert.Nil(t, mgr)
}

func TestNew_DefaultsToKeyPrefix(t *testing.T) {
	t.Parallel()
	_, plaintext := issueKey(t, mustManager(t), apikey.CreateParams{Subject: "u1"})
	assert.True(t, strings.HasPrefix(plaintext, "key_"))
}

// TestNilEffect_RejectedByEveryOperation pins that a missing effect is an
// error, never a nil dereference. TouchFunc is the one exception and has
// its own test.
func TestNilEffect_RejectedByEveryOperation(t *testing.T) {
	t.Parallel()
	mgr := mustManager(t)
	ctx := context.Background()
	someID := id.UUID{15: 1}

	t.Run("Create save", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(mgr.Create(ctx, apikey.CreateParams{Subject: "u1"}, nil))
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Get load", func(t *testing.T) {
		t.Parallel()
		_, err := mgr.Get(ctx, someID, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("List list", func(t *testing.T) {
		t.Parallel()
		_, err := mgr.List(ctx, apikey.Filter{}, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Revoke revoke", func(t *testing.T) {
		t.Parallel()
		err := mgr.Revoke(ctx, someID, loadsKey(apikey.Key{}), nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Rotate swap", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(mgr.Rotate(ctx, someID, 0, loadsKey(apikey.Key{}), nil))
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Verify load", func(t *testing.T) {
		t.Parallel()
		_, err := mgr.Verify(ctx, "key_whatever", nil, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})

	t.Run("Verifier load", func(t *testing.T) {
		t.Parallel()
		_, err := mgr.Verifier(nil, nil)
		assert.ErrorIs(t, err, apikey.ErrNilEffect)
	})
}

// TestNilEffect_CheckedBeforeAnyWork pins that a wiring error costs no
// work: no key is minted and no tenant scope hook runs.
func TestNilEffect_CheckedBeforeAnyWork(t *testing.T) {
	t.Parallel()
	scopeCalled := false
	mgr := mustManager(t, apikey.WithScope(func(context.Context) (string, error) {
		scopeCalled = true
		return "t1", nil
	}))

	_, _, err := expose(mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"}, nil))
	require.ErrorIs(t, err, apikey.ErrNilEffect)
	assert.False(t, scopeCalled)
}
