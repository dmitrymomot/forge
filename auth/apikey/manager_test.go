package apikey_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
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

func TestGetRevoke(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store)
	ctx := context.Background()
	k, plaintext, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	got, err := mgr.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, "u1", got.Subject)

	require.NoError(t, mgr.Revoke(ctx, k.ID))
	got, err = mgr.Get(ctx, k.ID)
	require.NoError(t, err)
	assert.False(t, got.RevokedAt.IsZero())

	_, err = mgr.Verify(ctx, plaintext)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)

	assert.ErrorIs(t, mgr.Revoke(ctx, id.UUID{15: 9}), apikey.ErrNotFound)
}

func TestRotate(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk_live"))
	ctx := context.Background()
	old, oldPlain, err := mgr.Create(ctx, apikey.CreateParams{
		Subject: "u1", Tenant: "t1", Name: "prod", Scopes: []string{"a"}, Meta: map[string]string{"m": "1"},
	})
	require.NoError(t, err)

	grace := time.Hour
	before := time.Now().UTC()
	fresh, freshPlain, err := mgr.Rotate(ctx, old.ID, grace)
	require.NoError(t, err)

	// Inheritance.
	assert.Equal(t, old.Subject, fresh.Subject)
	assert.Equal(t, old.Tenant, fresh.Tenant)
	assert.Equal(t, old.Name, fresh.Name)
	assert.Equal(t, old.Scopes, fresh.Scopes)
	assert.Equal(t, old.Meta, fresh.Meta)
	assert.NotEqual(t, old.ID, fresh.ID)
	assert.NotEqual(t, oldPlain, freshPlain)

	// Overlap: both verify during grace.
	_, err = mgr.Verify(ctx, oldPlain)
	require.NoError(t, err)
	_, err = mgr.Verify(ctx, freshPlain)
	require.NoError(t, err)

	// Old key's expiry ≈ now+grace.
	oldStored, err := mgr.Get(ctx, old.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, before.Add(grace), oldStored.ExpiresAt, 5*time.Second)
}

func TestRotate_ZeroGraceCutsOver(t *testing.T) {
	t.Parallel()
	mgr := apikey.New(apikey.NewMemoryStore())
	ctx := context.Background()
	old, oldPlain, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	_, freshPlain, err := mgr.Rotate(ctx, old.ID, 0)
	require.NoError(t, err)

	_, err = mgr.Verify(ctx, oldPlain)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
	_, err = mgr.Verify(ctx, freshPlain)
	assert.NoError(t, err)
}

func TestRotate_DeadKeysRejected(t *testing.T) {
	t.Parallel()
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store)
	ctx := context.Background()

	revoked, _, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	require.NoError(t, mgr.Revoke(ctx, revoked.ID))
	_, _, err = mgr.Rotate(ctx, revoked.ID, time.Hour)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)

	expired, _, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u2"})
	require.NoError(t, err)
	require.NoError(t, store.Expire(ctx, expired.ID, time.Now().UTC().Add(-time.Minute)))
	_, _, err = mgr.Rotate(ctx, expired.ID, time.Hour)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}

// errExpireBoom is a sentinel returned by expireFailStore.Expire, used to
// prove Rotate compensates for the failure rather than stranding a live,
// unreferenceable replacement key.
var errExpireBoom = errors.New("expire boom")

// expireFailStore wraps a real Store, always failing Expire, and records
// the id passed to Revoke so the test can prove Rotate compensates the
// freshly-minted replacement — never the old key, which must stay valid.
type expireFailStore struct {
	apikey.Store
	revokedID id.UUID
}

func (s *expireFailStore) Expire(context.Context, id.UUID, time.Time) error {
	return errExpireBoom
}

func (s *expireFailStore) Revoke(ctx context.Context, keyID id.UUID, at time.Time) error {
	s.revokedID = keyID
	return s.Store.Revoke(ctx, keyID, at)
}

func TestRotate_ExpireFailureRevokesReplacement(t *testing.T) {
	t.Parallel()
	mem := apikey.NewMemoryStore()
	fake := &expireFailStore{Store: mem}
	mgr := apikey.New(fake)
	ctx := context.Background()

	old, oldPlain, err := mgr.Create(ctx, apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	_, _, err = mgr.Rotate(ctx, old.ID, time.Hour)
	require.Error(t, err)
	assert.ErrorIs(t, err, errExpireBoom)

	// Availability: the old key is untouched and still verifies.
	_, err = mgr.Verify(ctx, oldPlain)
	require.NoError(t, err)

	// Compensation ran on the replacement's id, not the old key's.
	require.False(t, fake.revokedID.IsZero())
	assert.NotEqual(t, old.ID, fake.revokedID)

	replacement, err := mem.Get(ctx, fake.revokedID)
	require.NoError(t, err)
	assert.False(t, replacement.RevokedAt.IsZero())
}
