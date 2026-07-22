package invite_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/invite"
	"github.com/dmitrymomot/forge/core/id"
)

// mkInvite builds a record with a handcrafted ID so ordering tests are
// deterministic (UUIDv7 ids from the same millisecond have random tails).
func mkInvite(b byte, hash, email, tenant string) invite.Invite {
	now := time.Now().UTC()
	return invite.Invite{
		ID:        id.UUID{15: b}, // last byte varies → byte-ascending = ascending b
		Hash:      hash,
		Email:     email,
		Tenant:    tenant,
		Role:      "member",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
}

func TestMemoryStore_CreateGet(t *testing.T) {
	t.Parallel()
	s := invite.NewMemoryStore()
	ctx := context.Background()
	inv := mkInvite(1, "h1", "a@b.com", "t1")

	require.NoError(t, s.Create(ctx, inv))

	got, err := s.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, inv, got)

	byHash, err := s.GetByHash(ctx, "h1")
	require.NoError(t, err)
	assert.Equal(t, inv.ID, byHash.ID)
}

func TestMemoryStore_Duplicate(t *testing.T) {
	t.Parallel()
	s := invite.NewMemoryStore()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, mkInvite(1, "h1", "a@b.com", "t1")))

	assert.ErrorIs(t, s.Create(ctx, mkInvite(1, "h2", "a@b.com", "t1")), invite.ErrDuplicate)
	assert.ErrorIs(t, s.Create(ctx, mkInvite(2, "h1", "a@b.com", "t1")), invite.ErrDuplicate)
}

func TestMemoryStore_NotFound(t *testing.T) {
	t.Parallel()
	s := invite.NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	missing := id.UUID{15: 9}

	_, err := s.Get(ctx, missing)
	assert.ErrorIs(t, err, invite.ErrNotFound)
	_, err = s.GetByHash(ctx, "missing")
	assert.ErrorIs(t, err, invite.ErrNotFound)
	assert.ErrorIs(t, s.Accept(ctx, missing, now), invite.ErrNotFound)
	assert.ErrorIs(t, s.Revoke(ctx, missing, now), invite.ErrNotFound)
	assert.ErrorIs(t, s.Rotate(ctx, missing, "h2", now), invite.ErrNotFound)
}

func TestMemoryStore_AcceptClassification(t *testing.T) {
	t.Parallel()
	s := invite.NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()

	pending := mkInvite(1, "h1", "a@b.com", "t1")
	require.NoError(t, s.Create(ctx, pending))
	require.NoError(t, s.Accept(ctx, pending.ID, now))
	assert.ErrorIs(t, s.Accept(ctx, pending.ID, now), invite.ErrAlreadyAccepted)

	revoked := mkInvite(2, "h2", "a@b.com", "t1")
	require.NoError(t, s.Create(ctx, revoked))
	require.NoError(t, s.Revoke(ctx, revoked.ID, now))
	assert.ErrorIs(t, s.Accept(ctx, revoked.ID, now), invite.ErrRevoked)

	expired := mkInvite(3, "h3", "a@b.com", "t1")
	require.NoError(t, s.Create(ctx, expired))
	assert.ErrorIs(t, s.Accept(ctx, expired.ID, expired.ExpiresAt), invite.ErrExpired)
	// Boundary: at is strictly before ExpiresAt → acceptable.
	require.NoError(t, s.Accept(ctx, expired.ID, expired.ExpiresAt.Add(-time.Nanosecond)))
}

func TestMemoryStore_RevokeClassification(t *testing.T) {
	t.Parallel()
	s := invite.NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()

	inv := mkInvite(1, "h1", "a@b.com", "t1")
	require.NoError(t, s.Create(ctx, inv))
	require.NoError(t, s.Revoke(ctx, inv.ID, now))
	require.NoError(t, s.Revoke(ctx, inv.ID, now.Add(time.Minute))) // idempotent
	got, err := s.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, now, got.RevokedAt, "second revoke keeps original timestamp")

	accepted := mkInvite(2, "h2", "a@b.com", "t1")
	require.NoError(t, s.Create(ctx, accepted))
	require.NoError(t, s.Accept(ctx, accepted.ID, now))
	assert.ErrorIs(t, s.Revoke(ctx, accepted.ID, now), invite.ErrAlreadyAccepted)
}

func TestMemoryStore_Rotate(t *testing.T) {
	t.Parallel()
	s := invite.NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()

	inv := mkInvite(1, "h1", "a@b.com", "t1")
	require.NoError(t, s.Create(ctx, inv))
	later := now.Add(2 * time.Hour)
	require.NoError(t, s.Rotate(ctx, inv.ID, "h1b", later))

	got, err := s.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, "h1b", got.Hash)
	assert.Equal(t, later, got.ExpiresAt)

	// The old hash no longer resolves; the new one does.
	_, err = s.GetByHash(ctx, "h1")
	assert.ErrorIs(t, err, invite.ErrNotFound)
	byHash, err := s.GetByHash(ctx, "h1b")
	require.NoError(t, err)
	assert.Equal(t, inv.ID, byHash.ID)

	// Rotating to another record's hash is a duplicate.
	other := mkInvite(2, "h2", "b@b.com", "t1")
	require.NoError(t, s.Create(ctx, other))
	assert.ErrorIs(t, s.Rotate(ctx, inv.ID, "h2", later), invite.ErrDuplicate)

	// Accepted and revoked invites refuse rotation.
	require.NoError(t, s.Accept(ctx, other.ID, now))
	assert.ErrorIs(t, s.Rotate(ctx, other.ID, "h2b", later), invite.ErrAlreadyAccepted)
	revoked := mkInvite(3, "h3", "c@b.com", "t1")
	require.NoError(t, s.Create(ctx, revoked))
	require.NoError(t, s.Revoke(ctx, revoked.ID, now))
	assert.ErrorIs(t, s.Rotate(ctx, revoked.ID, "h3b", later), invite.ErrRevoked)
}

func TestMemoryStore_List(t *testing.T) {
	t.Parallel()
	s := invite.NewMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()

	a := mkInvite(1, "h1", "a@b.com", "t1")
	b := mkInvite(2, "h2", "b@b.com", "t1")
	c := mkInvite(3, "h3", "a@b.com", "t2")
	expired := mkInvite(4, "h4", "d@b.com", "t1")
	expired.ExpiresAt = now.Add(-time.Minute)
	for _, inv := range []invite.Invite{a, b, c, expired} {
		require.NoError(t, s.Create(ctx, inv))
	}
	require.NoError(t, s.Accept(ctx, b.ID, now))

	all, err := s.List(ctx, invite.Filter{})
	require.NoError(t, err)
	require.Len(t, all, 4)
	// Newest first by id bytes: 4, 3, 2, 1.
	assert.Equal(t, []id.UUID{expired.ID, c.ID, b.ID, a.ID},
		[]id.UUID{all[0].ID, all[1].ID, all[2].ID, all[3].ID})

	t1, err := s.List(ctx, invite.Filter{Tenant: "t1"})
	require.NoError(t, err)
	assert.Len(t, t1, 3)

	byEmail, err := s.List(ctx, invite.Filter{Email: "a@b.com"})
	require.NoError(t, err)
	assert.Len(t, byEmail, 2)

	// Pending excludes the accepted and the expired record.
	pending, err := s.List(ctx, invite.Filter{Pending: true})
	require.NoError(t, err)
	require.Len(t, pending, 2)
	assert.ElementsMatch(t, []id.UUID{a.ID, c.ID}, []id.UUID{pending[0].ID, pending[1].ID})
}
