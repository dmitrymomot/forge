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

func TestGet_ReturnsLoadedRecord(t *testing.T) {
	t.Parallel()
	want := apikey.Key{ID: id.UUID{15: 1}, Subject: "u1"}

	got, err := apikey.Get(context.Background(), mustConfig(t), want.ID, loadsKey(want))
	require.NoError(t, err)
	assert.Equal(t, want.Subject, got.Subject)
}

func TestGet_PassesRequestedIDToLoad(t *testing.T) {
	t.Parallel()
	want := id.UUID{15: 7}
	var asked id.UUID

	_, err := apikey.Get(context.Background(), mustConfig(t), want,
		func(_ context.Context, keyID id.UUID) (apikey.Key, error) {
			asked = keyID
			return apikey.Key{ID: keyID}, nil
		})
	require.NoError(t, err)
	assert.Equal(t, want, asked)
}

func TestGet_PropagatesLoadError(t *testing.T) {
	t.Parallel()
	_, err := apikey.Get(context.Background(), mustConfig(t), id.UUID{15: 1},
		func(context.Context, id.UUID) (apikey.Key, error) { return apikey.Key{}, apikey.ErrNotFound })
	assert.ErrorIs(t, err, apikey.ErrNotFound)
}

func TestList_PassesFilterToEffect(t *testing.T) {
	t.Parallel()
	want := apikey.Filter{Subject: "u1", Tenant: "t1"}
	var asked apikey.Filter

	_, err := apikey.List(context.Background(), mustConfig(t), want,
		func(_ context.Context, f apikey.Filter) ([]apikey.Key, error) {
			asked = f
			return nil, nil
		})
	require.NoError(t, err)
	assert.Equal(t, want, asked)
}

func TestList_ReturnsEffectResult(t *testing.T) {
	t.Parallel()
	want := []apikey.Key{{Subject: "u1"}, {Subject: "u2"}}

	got, err := apikey.List(context.Background(), mustConfig(t), apikey.Filter{},
		func(context.Context, apikey.Filter) ([]apikey.Key, error) { return want, nil })
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestList_PropagatesEffectError(t *testing.T) {
	t.Parallel()
	errBackend := errors.New("backend down")

	_, err := apikey.List(context.Background(), mustConfig(t), apikey.Filter{},
		func(context.Context, apikey.Filter) ([]apikey.Key, error) { return nil, errBackend })
	assert.ErrorIs(t, err, errBackend)
}

func TestRevoke_StampsTheRequestedKey(t *testing.T) {
	t.Parallel()
	target := id.UUID{15: 3}
	var stampedID id.UUID
	var stampedAt time.Time
	before := time.Now().UTC()

	err := apikey.Revoke(context.Background(), mustConfig(t), target, loadsKey(apikey.Key{ID: target}),
		func(_ context.Context, keyID id.UUID, at time.Time) error {
			stampedID, stampedAt = keyID, at
			return nil
		})
	require.NoError(t, err)
	assert.Equal(t, target, stampedID)
	assert.WithinDuration(t, before, stampedAt, 5*time.Second)
}

// TestRevoke_ResolvesRecordBeforeStamping pins why Revoke takes a load
// effect: an unknown key must fail before any write runs.
func TestRevoke_ResolvesRecordBeforeStamping(t *testing.T) {
	t.Parallel()
	stamped := false

	err := apikey.Revoke(context.Background(), mustConfig(t), id.UUID{15: 9},
		func(context.Context, id.UUID) (apikey.Key, error) { return apikey.Key{}, apikey.ErrNotFound },
		func(context.Context, id.UUID, time.Time) error {
			stamped = true
			return nil
		})
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.False(t, stamped)
}

func TestRevoke_PropagatesStampError(t *testing.T) {
	t.Parallel()
	errBackend := errors.New("backend down")

	err := apikey.Revoke(context.Background(), mustConfig(t), id.UUID{15: 1}, loadsKey(apikey.Key{}),
		func(context.Context, id.UUID, time.Time) error { return errBackend })
	assert.ErrorIs(t, err, errBackend)
}
