package apikey_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
)

func TestNew_Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { apikey.New(nil) })
	assert.Panics(t, func() { apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("")) })
	assert.Panics(t, func() { apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("SK-Live")) })
}

func TestCreate_KeyAnatomy(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))

	k, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy", Scopes: []string{"deploy:write"},
	})
	require.NoError(t, err)

	// <prefix>_<43 payload><6 checksum>
	assert.Len(t, plaintext, len("sk_live")+1+43+6)
	assert.True(t, strings.HasPrefix(plaintext, "sk_live_"))
	for _, c := range plaintext[len("sk_live_"):] {
		assert.Contains(t, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", string(c))
	}
	assert.Equal(t, plaintext[:12], k.Preview)
	assert.NotContains(t, k.Hash, plaintext[8:20]) // hash, not plaintext, at rest
	assert.False(t, k.ID.IsZero())
	assert.False(t, k.CreatedAt.IsZero())

	stored, err := store.Get(context.Background(), k.ID)
	require.NoError(t, err)
	assert.Equal(t, "user_42", stored.Subject)
	assert.Equal(t, "org_7", stored.Tenant)
}

func TestCreate_SubjectRequired(t *testing.T) {
	t.Parallel()
	mgr := apikey.New(apikey.NewMemoryStore())
	_, _, err := mgr.Create(context.Background(), apikey.CreateParams{})
	assert.ErrorIs(t, err, apikey.ErrSubjectRequired)
}

func TestCreate_DefaultPrefix(t *testing.T) {
	t.Parallel()
	mgr := apikey.New(apikey.NewMemoryStore())
	_, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(plaintext, "key_"))
}
