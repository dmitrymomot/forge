package apikey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/core/id"
)

// verifiable mints one live key and its credential under a "sk_live"
// config.
func verifiable(t *testing.T) (*apikey.Manager, apikey.Key, string) {
	t.Helper()
	mgr := mustManager(t, apikey.WithPrefix("sk_live"))
	k, plaintext := issueKey(t, mgr, apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy",
		Scopes: []string{"deploy:write"}, Meta: map[string]string{"env": "prod"},
	})
	return mgr, k, plaintext
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

func TestVerify_ResolvesSubjectAndTenant(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	identity, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	require.NoError(t, err)
	assert.Equal(t, "user_42", identity.Subject)
	assert.Equal(t, "org_7", identity.Tenant)
}

func TestVerify_ReportsAPIKeyMethod(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	identity, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	require.NoError(t, err)
	assert.Equal(t, guard.MethodAPIKey, identity.Method)
}

func TestVerify_CarriesScopesAndMeta(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	identity, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"deploy:write"}, identity.Scopes)
	assert.Equal(t, "prod", identity.Meta["env"])
	assert.Equal(t, k.ID.String(), identity.Meta["key_id"])
	assert.Equal(t, "CI deploy", identity.Meta["key_name"])
}

func TestVerify_RejectsMalformedCredential(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	for name, credential := range map[string]string{
		"empty":        "",
		"prefix only":  "sk_live_",
		"truncated":    plaintext[:len(plaintext)-1],
		"bad checksum": tamper(plaintext),
		"wrong prefix": "sk_test" + plaintext[len("sk_live"):],
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mgr.Verify(context.Background(), credential, loadsKeyByHash(k), nil)
			assert.ErrorIs(t, err, apikey.ErrMalformedKey)
		})
	}
}

// TestVerify_RejectsMalformedBeforeLoading pins the DoS guard: credential
// stuffing never reaches storage.
func TestVerify_RejectsMalformedBeforeLoading(t *testing.T) {
	t.Parallel()
	mgr, _, plaintext := verifiable(t)
	loaded := false

	_, err := mgr.Verify(context.Background(), tamper(plaintext),
		func(context.Context, string) (apikey.Key, error) {
			loaded = true
			return apikey.Key{}, nil
		}, nil)
	require.ErrorIs(t, err, apikey.ErrMalformedKey)
	assert.False(t, loaded)
}

func TestVerify_RejectsUnknownCredential(t *testing.T) {
	t.Parallel()
	mgr, _, plaintext := verifiable(t)

	_, err := mgr.Verify(context.Background(), plaintext,
		func(context.Context, string) (apikey.Key, error) { return apikey.Key{}, apikey.ErrNotFound }, nil)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_RejectsRevokedKey(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	k.RevokedAt = time.Now().UTC()

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)
}

func TestVerify_RejectsExpiredKey(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	k.ExpiresAt = time.Now().UTC().Add(-time.Second)

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}

func TestVerify_AcceptsFutureExpiry(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	k.ExpiresAt = time.Now().UTC().Add(time.Hour)

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	assert.NoError(t, err)
}

// TestVerify_RejectsRecordWithMismatchedHash pins the defence against a
// buggy load effect returning the wrong row.
func TestVerify_RejectsRecordWithMismatchedHash(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	other := k
	other.Hash = "0000000000000000000000000000000000000000000000000000000000000000"

	_, err := mgr.Verify(context.Background(), plaintext,
		func(context.Context, string) (apikey.Key, error) { return other, nil }, nil)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_RejectsRecordWithoutSubject(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	k.Subject = ""

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	assert.ErrorIs(t, err, apikey.ErrKeyNotFound)
}

func TestVerify_WrapsLoadError(t *testing.T) {
	t.Parallel()
	mgr, _, plaintext := verifiable(t)
	errBackend := errors.New("backend down")

	_, err := mgr.Verify(context.Background(), plaintext,
		func(context.Context, string) (apikey.Key, error) { return apikey.Key{}, errBackend }, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errBackend)
}

// TestVerify_ReservedMetaKeysOverridden pins that key_id and key_name come
// from the record itself, overriding any values stashed in stored Meta
// under those keys.
func TestVerify_ReservedMetaKeysOverridden(t *testing.T) {
	t.Parallel()
	mgr := mustManager(t, apikey.WithPrefix("sk_live"))
	k, plaintext := issueKey(t, mgr, apikey.CreateParams{
		Subject: "user_42", Name: "CI deploy",
		Meta: map[string]string{"key_id": "spoofed", "key_name": "spoofed"},
	})

	identity, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	require.NoError(t, err)
	assert.Equal(t, k.ID.String(), identity.Meta["key_id"])
	assert.Equal(t, "CI deploy", identity.Meta["key_name"])
}

// TestVerify_IdentityDoesNotAliasRecord pins that a caller mutating one
// identity cannot corrupt the next verify's result.
func TestVerify_IdentityDoesNotAliasRecord(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	load := loadsKeyByHash(k)

	first, err := mgr.Verify(context.Background(), plaintext, load, nil)
	require.NoError(t, err)
	first.Meta["env"] = "mutated"
	first.Scopes[0] = "mutated"

	second, err := mgr.Verify(context.Background(), plaintext, load, nil)
	require.NoError(t, err)
	assert.Equal(t, "prod", second.Meta["env"])
	assert.Equal(t, "deploy:write", second.Scopes[0])
}

func TestVerify_TouchesNeverUsedKey(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	touched := false

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k),
		func(context.Context, id.UUID, time.Time) error {
			touched = true
			return nil
		})
	require.NoError(t, err)
	assert.True(t, touched)
}

func TestVerify_SkipsTouchForFreshRecord(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	k.LastUsedAt = time.Now().UTC()
	touched := false

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k),
		func(context.Context, id.UUID, time.Time) error {
			touched = true
			return nil
		})
	require.NoError(t, err)
	assert.False(t, touched)
}

func TestVerify_TouchesStaleRecord(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)
	k.LastUsedAt = time.Now().UTC().Add(-2 * time.Minute)
	var stampedID id.UUID

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k),
		func(_ context.Context, keyID id.UUID, _ time.Time) error {
			stampedID = keyID
			return nil
		})
	require.NoError(t, err)
	assert.Equal(t, k.ID, stampedID)
}

func TestVerify_ZeroIntervalTouchesFreshRecord(t *testing.T) {
	t.Parallel()
	mgr := mustManager(t, apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(0))
	k, plaintext := issueKey(t, mgr, apikey.CreateParams{Subject: "u1"})
	k.LastUsedAt = time.Now().UTC()
	touched := false

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k),
		func(context.Context, id.UUID, time.Time) error {
			touched = true
			return nil
		})
	require.NoError(t, err)
	assert.True(t, touched)
}

func TestVerify_NegativeIntervalDisablesTouch(t *testing.T) {
	t.Parallel()
	mgr := mustManager(t, apikey.WithPrefix("sk_live"), apikey.WithTouchInterval(-1))
	k, plaintext := issueKey(t, mgr, apikey.CreateParams{Subject: "u1"})
	touched := false

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k),
		func(context.Context, id.UUID, time.Time) error {
			touched = true
			return nil
		})
	require.NoError(t, err)
	assert.False(t, touched)
}

func TestVerify_NilTouchIsAccepted(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	_, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k), nil)
	assert.NoError(t, err)
}

// TestVerify_TouchFailureDoesNotFailAuthentication pins that last-used
// tracking is observability: losing it must not lock a caller out.
func TestVerify_TouchFailureDoesNotFailAuthentication(t *testing.T) {
	t.Parallel()
	mgr, k, plaintext := verifiable(t)

	identity, err := mgr.Verify(context.Background(), plaintext, loadsKeyByHash(k),
		func(context.Context, id.UUID, time.Time) error { return errors.New("backend down") })
	require.NoError(t, err)
	assert.Equal(t, "user_42", identity.Subject)
}
