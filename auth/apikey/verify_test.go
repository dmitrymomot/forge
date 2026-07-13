package apikey_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/guard"
)

// issue returns a manager over its own memory store plus one minted key.
func issue(t *testing.T, opts ...apikey.Option) (*apikey.Manager, apikey.Store, apikey.Key, string) {
	t.Helper()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, append([]apikey.Option{apikey.WithPrefix("sk_live")}, opts...)...)
	k, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
		Scopes: []string{"deploy:write"}, Meta: map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	return mgr, store, k, plaintext
}

// tamper flips the final checksum character to a different base62 char.
func tamper(s string) string {
	last := s[len(s)-1]
	repl := byte('a')
	if last == 'a' {
		repl = 'b'
	}
	return s[:len(s)-1] + string(repl)
}

func TestVerify_OK(t *testing.T) {
	t.Parallel()
	mgr, _, k, plaintext := issue(t)

	identity, err := mgr.Verify(context.Background(), plaintext)
	require.NoError(t, err)
	assert.Equal(t, "user_42", identity.Subject)
	assert.Equal(t, "org_7", identity.Tenant)
	assert.Equal(t, guard.MethodAPIKey, identity.Method)
	assert.Equal(t, []string{"deploy:write"}, identity.Scopes)
	assert.Equal(t, k.ID.String(), identity.Meta["key_id"])
	assert.Equal(t, "CI deploy", identity.Meta["key_name"])
	assert.Equal(t, "prod", identity.Meta["env"])
}

func TestVerify_Malformed(t *testing.T) {
	t.Parallel()
	mgr, _, _, plaintext := issue(t)
	ctx := context.Background()

	for name, cred := range map[string]string{
		"empty":        "",
		"prefix only":  "sk_live_",
		"truncated":    plaintext[:len(plaintext)-1],
		"bad checksum": tamper(plaintext),
		"wrong prefix": "sk_test" + plaintext[len("sk_live"):],
	} {
		_, err := mgr.Verify(ctx, cred)
		assert.ErrorIs(t, err, apikey.ErrMalformedKey, name)
	}
}

func TestVerify_UnknownKey(t *testing.T) {
	t.Parallel()
	mgr, _, _, _ := issue(t)
	// A second issuer with the same prefix mints a structurally valid key
	// that the first store has never seen.
	other := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"))
	_, foreign, err := other.Create(context.Background(), apikey.CreateParams{Subject: "u9"})
	require.NoError(t, err)

	_, err = mgr.Verify(context.Background(), foreign)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_Revoked(t *testing.T) {
	t.Parallel()
	mgr, store, k, plaintext := issue(t)
	require.NoError(t, store.Revoke(context.Background(), k.ID, time.Now().UTC()))

	_, err := mgr.Verify(context.Background(), plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)
}

func TestVerify_Expired(t *testing.T) {
	t.Parallel()
	mgr, store, k, plaintext := issue(t)
	ctx := context.Background()

	require.NoError(t, store.Expire(ctx, k.ID, time.Now().UTC().Add(time.Hour)))
	_, err := mgr.Verify(ctx, plaintext) // future expiry still verifies
	require.NoError(t, err)

	require.NoError(t, store.Expire(ctx, k.ID, time.Now().UTC().Add(-time.Second)))
	_, err = mgr.Verify(ctx, plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}

func TestVerify_EmptySubjectRecordRejected(t *testing.T) {
	t.Parallel()
	// A corrupt record (empty Subject) seeded directly into the store must
	// not authenticate, even though guard would also catch it.
	_, _, k, plaintext := issue(t)
	ctx := context.Background()

	corrupt := k
	corrupt.Subject = ""
	store := apikey.NewMemoryStore()
	require.NoError(t, store.Create(ctx, corrupt))
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))

	_, err := mgr.Verify(ctx, plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_TouchThrottling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("default touches never-used key once", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t)
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		first := got.LastUsedAt
		assert.False(t, first.IsZero())

		// Immediately re-verify: fresher than 60s → untouched.
		_, err = mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err = store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.Equal(first))
	})

	t.Run("stale record is touched", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t)
		old := time.Now().UTC().Add(-2 * time.Minute)
		require.NoError(t, store.Touch(ctx, k.ID, old))
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.After(old))
	})

	t.Run("negative interval disables tracking", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t, apikey.WithTouchInterval(-1))
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.IsZero())
	})

	t.Run("zero interval touches every request", func(t *testing.T) {
		t.Parallel()
		mgr, store, k, plaintext := issue(t, apikey.WithTouchInterval(0))
		recent := time.Now().UTC().Add(-time.Second)
		require.NoError(t, store.Touch(ctx, k.ID, recent))
		_, err := mgr.Verify(ctx, plaintext)
		require.NoError(t, err)
		got, err := store.Get(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.After(recent))
	})
}

func TestVerify_ReservedMetaKeysOverridden(t *testing.T) {
	t.Parallel()
	// key_id and key_name are reserved: Verify must always set them from
	// the record's own ID/Name, overriding any attacker-supplied values
	// stashed in stored Meta under those same keys.
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))
	ctx := context.Background()

	k, plaintext, err := mgr.Create(ctx, apikey.CreateParams{
		Subject: "user_42",
		Name:    "CI deploy",
		Meta:    map[string]string{"key_id": "spoofed", "key_name": "spoofed"},
	})
	require.NoError(t, err)

	identity, err := mgr.Verify(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, k.ID.String(), identity.Meta["key_id"])
	assert.NotEqual(t, "spoofed", identity.Meta["key_id"])
	assert.Equal(t, "CI deploy", identity.Meta["key_name"])
	assert.NotEqual(t, "spoofed", identity.Meta["key_name"])
}

func TestVerify_IdentityMetaIsolated(t *testing.T) {
	t.Parallel()
	mgr, _, _, plaintext := issue(t)
	ctx := context.Background()

	id1, err := mgr.Verify(ctx, plaintext)
	require.NoError(t, err)
	id1.Meta["env"] = "mutated"
	id1.Scopes[0] = "mutated"

	id2, err := mgr.Verify(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, "prod", id2.Meta["env"])
	assert.Equal(t, "deploy:write", id2.Scopes[0])
}
