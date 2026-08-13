package apikey_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
)

// errStorageBoom stands in for an unexpected backend failure and proves
// Create and Verify wrap it with %w instead of swallowing it. A failing
// effect is a one-line closure now, not a whole fake type.
var errStorageBoom = errors.New("boom")

func TestCreate_EffectErrorWrapped(t *testing.T) {
	t.Parallel()
	_, _, err := apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"},
		func(context.Context, apikey.Key) error { return errStorageBoom })
	require.Error(t, err)
	assert.ErrorIs(t, err, errStorageBoom)
}

func TestVerify_EffectErrorWrapped(t *testing.T) {
	t.Parallel()
	cfg, _, _, plaintext := issue(t)

	_, err := apikey.Verify(context.Background(), cfg, plaintext,
		func(context.Context, string) (apikey.Key, error) { return apikey.Key{}, errStorageBoom },
		nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errStorageBoom)
}

func TestList_EffectErrorPropagates(t *testing.T) {
	t.Parallel()
	_, err := apikey.List(context.Background(), mustConfig(t), apikey.Filter{},
		func(context.Context, apikey.Filter) ([]apikey.Key, error) { return nil, errStorageBoom })
	assert.ErrorIs(t, err, errStorageBoom)
}
