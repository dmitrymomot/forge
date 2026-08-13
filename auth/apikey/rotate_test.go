package apikey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

// discardSwap is a SwapFunc that accepts every rotation.
func discardSwap(context.Context, id.UUID, time.Time, apikey.Key) error { return nil }

// rotatable returns a live key suitable as the old side of a rotation.
func rotatable(t *testing.T, cfg apikey.Config) apikey.Key {
	t.Helper()
	k, _ := issueKey(t, cfg, apikey.CreateParams{
		Subject: "u1", Tenant: "t1", Name: "prod",
		Scopes: []string{"a"}, Meta: map[string]string{"m": "1"},
	})
	return k
}

func TestRotate_ReplacementInheritsIdentityFields(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)

	fresh, _, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, time.Hour, loadsKey(old), discardSwap))
	require.NoError(t, err)

	assert.Equal(t, old.Subject, fresh.Subject)
	assert.Equal(t, old.Tenant, fresh.Tenant)
	assert.Equal(t, old.Name, fresh.Name)
	assert.Equal(t, old.Scopes, fresh.Scopes)
	assert.Equal(t, old.Meta, fresh.Meta)
}

// TestRotate_ReplacementDoesNotInheritExpiry pins that a rotation restarts
// the lifetime instead of copying the old key's deadline.
func TestRotate_ReplacementDoesNotInheritExpiry(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)
	old.ExpiresAt = time.Now().UTC().Add(time.Hour)

	fresh, _, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, time.Hour, loadsKey(old), discardSwap))
	require.NoError(t, err)
	assert.True(t, fresh.ExpiresAt.IsZero())
}

func TestRotate_ReplacementIsADistinctCredential(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old, oldPlain := issueKey(t, cfg, apikey.CreateParams{Subject: "u1"})

	fresh, freshPlain, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, time.Hour, loadsKey(old), discardSwap))
	require.NoError(t, err)

	assert.NotEqual(t, old.ID, fresh.ID)
	assert.NotEqual(t, old.Hash, fresh.Hash)
	assert.NotEqual(t, oldPlain, freshPlain)
}

func TestRotate_HandsBothWritesToSwap(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)
	grace := time.Hour
	before := time.Now().UTC()

	var swappedID id.UUID
	var swappedExpiry time.Time
	var swappedReplacement apikey.Key

	fresh, _, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, grace, loadsKey(old),
		func(_ context.Context, oldID id.UUID, oldExpiresAt time.Time, replacement apikey.Key) error {
			swappedID, swappedExpiry, swappedReplacement = oldID, oldExpiresAt, replacement
			return nil
		}))
	require.NoError(t, err)

	assert.Equal(t, old.ID, swappedID)
	assert.Equal(t, fresh.ID, swappedReplacement.ID)
	assert.WithinDuration(t, before.Add(grace), swappedExpiry, 5*time.Second)
}

func TestRotate_ZeroGraceExpiresOldKeyImmediately(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)
	before := time.Now().UTC()

	var swappedExpiry time.Time
	_, _, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, 0, loadsKey(old),
		func(_ context.Context, _ id.UUID, oldExpiresAt time.Time, _ apikey.Key) error {
			swappedExpiry = oldExpiresAt
			return nil
		}))
	require.NoError(t, err)
	assert.WithinDuration(t, before, swappedExpiry, 5*time.Second)
}

func TestRotate_RejectsRevokedKey(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)
	old.RevokedAt = time.Now().UTC()

	_, _, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, time.Hour, loadsKey(old), discardSwap))
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)
}

func TestRotate_RejectsExpiredKey(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)
	old.ExpiresAt = time.Now().UTC().Add(-time.Minute)

	_, _, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, time.Hour, loadsKey(old), discardSwap))
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}

// TestRotate_RejectsDeadKeyBeforeSwapping pins that a refused rotation
// never reaches the write.
func TestRotate_RejectsDeadKeyBeforeSwapping(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)
	old.RevokedAt = time.Now().UTC()
	swapped := false

	_, _, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, time.Hour, loadsKey(old),
		func(context.Context, id.UUID, time.Time, apikey.Key) error {
			swapped = true
			return nil
		}))
	require.Error(t, err)
	assert.False(t, swapped)
}

// TestRotate_FailedSwapReturnsNoPlaintext pins what the atomic SwapFunc
// buys: a failed rotation yields no credential to strand, so there is
// nothing to compensate for.
func TestRotate_FailedSwapReturnsNoPlaintext(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	old := rotatable(t, cfg)
	errSwap := errors.New("swap failed")

	fresh, freshPlain, err := expose(apikey.Rotate(context.Background(), cfg, old.ID, time.Hour, loadsKey(old),
		func(context.Context, id.UUID, time.Time, apikey.Key) error { return errSwap }))
	require.Error(t, err)
	assert.ErrorIs(t, err, errSwap)
	assert.Empty(t, freshPlain)
	assert.True(t, fresh.ID.IsZero())
}

func TestRotate_PropagatesLoadError(t *testing.T) {
	t.Parallel()
	_, _, err := expose(apikey.Rotate(context.Background(), mustConfig(t), id.UUID{15: 9}, time.Hour,
		func(context.Context, id.UUID) (apikey.Key, error) { return apikey.Key{}, apikey.ErrNotFound },
		discardSwap))
	assert.ErrorIs(t, err, apikey.ErrNotFound)
}
