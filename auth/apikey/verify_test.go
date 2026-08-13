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

// issue returns a Config over its own memory store plus one minted key.
func issue(t *testing.T, opts ...apikey.Option) (apikey.Config, *apikey.MemoryStore, apikey.Key, string) {
	t.Helper()
	cfg := mustConfig(t, append([]apikey.Option{apikey.WithPrefix("sk_live")}, opts...)...)
	mem := apikey.NewMemoryStore()
	k, plaintext, err := apikey.Create(context.Background(), cfg, apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
		Scopes: []string{"deploy:write"}, Meta: map[string]string{"env": "prod"},
	}, mem.Save)
	require.NoError(t, err)
	return cfg, mem, k, plaintext
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
	cfg, mem, k, plaintext := issue(t)

	identity, err := apikey.Verify(context.Background(), cfg, plaintext, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)
	assert.Equal(t, "user_42", identity.Subject)
	assert.Equal(t, "org_7", identity.Tenant)
	assert.Equal(t, guard.MethodAPIKey, identity.Method)
	assert.Equal(t, []string{"deploy:write"}, identity.Scopes)
	assert.Equal(t, k.ID.String(), identity.Meta["key_id"])
	assert.Equal(t, "CI deploy", identity.Meta["key_name"])
	assert.Equal(t, "prod", identity.Meta["env"])
}

func TestNewVerifier_ImplementsGuardSeam(t *testing.T) {
	t.Parallel()
	cfg, mem, _, plaintext := issue(t)

	verifier, err := apikey.NewVerifier(cfg, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)

	identity, err := verifier.Verify(context.Background(), plaintext)
	require.NoError(t, err)
	assert.Equal(t, "user_42", identity.Subject)

	_, err = verifier.Verify(context.Background(), tamper(plaintext))
	assert.ErrorIs(t, err, apikey.ErrMalformedKey)
}

func TestVerify_NilTouchDisablesTracking(t *testing.T) {
	t.Parallel()
	cfg, mem, k, plaintext := issue(t)
	ctx := context.Background()

	_, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, nil)
	require.NoError(t, err)

	got, err := mem.Load(ctx, k.ID)
	require.NoError(t, err)
	assert.True(t, got.LastUsedAt.IsZero())
}

func TestVerify_Malformed(t *testing.T) {
	t.Parallel()
	cfg, mem, _, plaintext := issue(t)
	ctx := context.Background()

	for name, cred := range map[string]string{
		"empty":        "",
		"prefix only":  "sk_live_",
		"truncated":    plaintext[:len(plaintext)-1],
		"bad checksum": tamper(plaintext),
		"wrong prefix": "sk_test" + plaintext[len("sk_live"):],
	} {
		_, err := apikey.Verify(ctx, cfg, cred, mem.LoadByHash, mem.Touch)
		assert.ErrorIs(t, err, apikey.ErrMalformedKey, name)
	}
}

func TestVerify_UnknownKey(t *testing.T) {
	t.Parallel()
	cfg, mem, _, _ := issue(t)
	storeThatThisVerifyNeverReads := apikey.NewMemoryStore()
	_, foreign, err := apikey.Create(context.Background(), cfg,
		apikey.CreateParams{Subject: "u9"}, storeThatThisVerifyNeverReads.Save)
	require.NoError(t, err)

	_, err = apikey.Verify(context.Background(), cfg, foreign, mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_Revoked(t *testing.T) {
	t.Parallel()
	cfg, mem, k, plaintext := issue(t)
	require.NoError(t, mem.Revoke(context.Background(), k.ID, time.Now().UTC()))

	_, err := apikey.Verify(context.Background(), cfg, plaintext, mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)
}

func TestVerify_Expired(t *testing.T) {
	t.Parallel()
	cfg, mem, k, plaintext := issue(t)
	ctx := context.Background()

	require.NoError(t, mem.Expire(ctx, k.ID, time.Now().UTC().Add(time.Hour)))
	_, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)

	require.NoError(t, mem.Expire(ctx, k.ID, time.Now().UTC().Add(-time.Second)))
	_, err = apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}

// TestVerify_EmptySubjectRecordRejected pins that a corrupt record (empty
// Subject) seeded directly into storage never authenticates, even though
// guard would also catch it.
func TestVerify_EmptySubjectRecordRejected(t *testing.T) {
	t.Parallel()
	cfg, _, k, plaintext := issue(t)
	ctx := context.Background()

	corrupt := k
	corrupt.Subject = ""
	mem := apikey.NewMemoryStore()
	require.NoError(t, mem.Save(ctx, corrupt))

	_, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_TouchThrottling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("default touches never-used key once", func(t *testing.T) {
		t.Parallel()
		cfg, mem, k, plaintext := issue(t)
		_, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
		require.NoError(t, err)
		got, err := mem.Load(ctx, k.ID)
		require.NoError(t, err)
		first := got.LastUsedAt
		assert.False(t, first.IsZero())

		_, err = apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
		require.NoError(t, err)
		got, err = mem.Load(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.Equal(first))
	})

	t.Run("stale record is touched", func(t *testing.T) {
		t.Parallel()
		cfg, mem, k, plaintext := issue(t)
		old := time.Now().UTC().Add(-2 * time.Minute)
		require.NoError(t, mem.Touch(ctx, k.ID, old))
		_, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
		require.NoError(t, err)
		got, err := mem.Load(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.After(old))
	})

	t.Run("negative interval disables tracking", func(t *testing.T) {
		t.Parallel()
		cfg, mem, k, plaintext := issue(t, apikey.WithTouchInterval(-1))
		_, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
		require.NoError(t, err)
		got, err := mem.Load(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.IsZero())
	})

	t.Run("zero interval touches every request", func(t *testing.T) {
		t.Parallel()
		cfg, mem, k, plaintext := issue(t, apikey.WithTouchInterval(0))
		recent := time.Now().UTC().Add(-time.Second)
		require.NoError(t, mem.Touch(ctx, k.ID, recent))
		_, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
		require.NoError(t, err)
		got, err := mem.Load(ctx, k.ID)
		require.NoError(t, err)
		assert.True(t, got.LastUsedAt.After(recent))
	})
}

// TestVerify_ReservedMetaKeysOverridden pins that key_id and key_name are
// reserved: Verify sets them from the record's own ID/Name, overriding any
// attacker-supplied values stashed in stored Meta under those keys.
func TestVerify_ReservedMetaKeysOverridden(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t, apikey.WithPrefix("sk_live"))
	mem := apikey.NewMemoryStore()
	ctx := context.Background()

	k, plaintext, err := apikey.Create(ctx, cfg, apikey.CreateParams{
		Subject: "user_42",
		Name:    "CI deploy",
		Meta:    map[string]string{"key_id": "spoofed", "key_name": "spoofed"},
	}, mem.Save)
	require.NoError(t, err)

	identity, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)
	assert.Equal(t, k.ID.String(), identity.Meta["key_id"])
	assert.NotEqual(t, "spoofed", identity.Meta["key_id"])
	assert.Equal(t, "CI deploy", identity.Meta["key_name"])
	assert.NotEqual(t, "spoofed", identity.Meta["key_name"])
}

func TestVerify_IdentityMetaIsolated(t *testing.T) {
	t.Parallel()
	cfg, mem, _, plaintext := issue(t)
	ctx := context.Background()

	id1, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)
	id1.Meta["env"] = "mutated"
	id1.Scopes[0] = "mutated"

	id2, err := apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)
	assert.Equal(t, "prod", id2.Meta["env"])
	assert.Equal(t, "deploy:write", id2.Scopes[0])
}
