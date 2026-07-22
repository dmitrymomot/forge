//go:build integration

package pgstore_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/invite"
	"github.com/dmitrymomot/forge/auth/invite/pgstore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ invite.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_invite_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

// mkInvite builds a record whose hash/email/tenant are unique per call:
// the table persists across test runs, so deterministic values would
// collide on the unique hash index or inflate List counts on re-runs.
func mkInvite(t *testing.T) invite.Invite {
	t.Helper()
	uid := id.NewUUID()
	now := time.Now().UTC().Truncate(time.Millisecond)
	return invite.Invite{
		ID:        uid,
		Hash:      "hash-" + uid.String(),
		Email:     "user-" + uid.String() + "@example.com",
		Tenant:    "tenant-" + uid.String(),
		Role:      "editor",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
}

func TestPg_CreateGetRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv := mkInvite(t)

	require.NoError(t, s.Create(ctx, inv))

	got, err := s.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, inv.Hash, got.Hash)
	assert.Equal(t, inv.Email, got.Email)
	assert.Equal(t, inv.Tenant, got.Tenant)
	assert.Equal(t, inv.Role, got.Role)
	assert.True(t, inv.CreatedAt.Equal(got.CreatedAt))
	assert.True(t, inv.ExpiresAt.Equal(got.ExpiresAt))
	assert.True(t, got.AcceptedAt.IsZero())
	assert.True(t, got.RevokedAt.IsZero())

	byHash, err := s.GetByHash(ctx, inv.Hash)
	require.NoError(t, err)
	assert.Equal(t, inv.ID, byHash.ID)
}

func TestPg_Duplicate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv := mkInvite(t)
	require.NoError(t, s.Create(ctx, inv))

	sameID := mkInvite(t)
	sameID.ID = inv.ID
	assert.ErrorIs(t, s.Create(ctx, sameID), invite.ErrDuplicate)

	sameHash := mkInvite(t)
	sameHash.Hash = inv.Hash
	assert.ErrorIs(t, s.Create(ctx, sameHash), invite.ErrDuplicate)
}

func TestPg_NotFound(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	missing := id.NewUUID()

	_, err := s.Get(ctx, missing)
	assert.ErrorIs(t, err, invite.ErrNotFound)
	_, err = s.GetByHash(ctx, "missing-"+missing.String())
	assert.ErrorIs(t, err, invite.ErrNotFound)
	assert.ErrorIs(t, s.Accept(ctx, missing, now), invite.ErrNotFound)
	assert.ErrorIs(t, s.Revoke(ctx, missing, now), invite.ErrNotFound)
	assert.ErrorIs(t, s.Rotate(ctx, missing, "h-"+missing.String(), now), invite.ErrNotFound)
}

func TestPg_AcceptClassification(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	inv := mkInvite(t)
	require.NoError(t, s.Create(ctx, inv))
	require.NoError(t, s.Accept(ctx, inv.ID, now))
	got, err := s.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.True(t, now.Equal(got.AcceptedAt))
	assert.ErrorIs(t, s.Accept(ctx, inv.ID, now), invite.ErrAlreadyAccepted)

	revoked := mkInvite(t)
	require.NoError(t, s.Create(ctx, revoked))
	require.NoError(t, s.Revoke(ctx, revoked.ID, now))
	assert.ErrorIs(t, s.Accept(ctx, revoked.ID, now), invite.ErrRevoked)

	expired := mkInvite(t)
	require.NoError(t, s.Create(ctx, expired))
	assert.ErrorIs(t, s.Accept(ctx, expired.ID, expired.ExpiresAt), invite.ErrExpired)
}

func TestPg_AcceptConcurrentSingleUse(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	inv := mkInvite(t)
	require.NoError(t, s.Create(ctx, inv))

	const n = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners int
	for range n {
		wg.Go(func() {
			err := s.Accept(ctx, inv.ID, time.Now().UTC())
			if err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
				return
			}
			assert.ErrorIs(t, err, invite.ErrAlreadyAccepted)
		})
	}
	wg.Wait()
	assert.Equal(t, 1, winners, "exactly one concurrent accept must win")
}

func TestPg_RevokeClassification(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	inv := mkInvite(t)
	require.NoError(t, s.Create(ctx, inv))
	require.NoError(t, s.Revoke(ctx, inv.ID, now))
	require.NoError(t, s.Revoke(ctx, inv.ID, now.Add(time.Minute))) // idempotent
	got, err := s.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.True(t, now.Equal(got.RevokedAt), "second revoke keeps original timestamp")

	accepted := mkInvite(t)
	require.NoError(t, s.Create(ctx, accepted))
	require.NoError(t, s.Accept(ctx, accepted.ID, now))
	assert.ErrorIs(t, s.Revoke(ctx, accepted.ID, now), invite.ErrAlreadyAccepted)
}

func TestPg_Rotate(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	inv := mkInvite(t)
	require.NoError(t, s.Create(ctx, inv))
	fresh := "rotated-" + inv.ID.String()
	later := now.Add(2 * time.Hour)
	require.NoError(t, s.Rotate(ctx, inv.ID, fresh, later))

	got, err := s.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, fresh, got.Hash)
	assert.True(t, later.Equal(got.ExpiresAt))
	_, err = s.GetByHash(ctx, inv.Hash)
	assert.ErrorIs(t, err, invite.ErrNotFound)

	// Rotating to another record's hash trips the unique index.
	other := mkInvite(t)
	require.NoError(t, s.Create(ctx, other))
	assert.ErrorIs(t, s.Rotate(ctx, inv.ID, other.Hash, later), invite.ErrDuplicate)

	// Accepted and revoked invites refuse rotation.
	require.NoError(t, s.Accept(ctx, other.ID, now))
	assert.ErrorIs(t, s.Rotate(ctx, other.ID, "x-"+other.ID.String(), later), invite.ErrAlreadyAccepted)
	revoked := mkInvite(t)
	require.NoError(t, s.Create(ctx, revoked))
	require.NoError(t, s.Revoke(ctx, revoked.ID, now))
	assert.ErrorIs(t, s.Rotate(ctx, revoked.ID, "y-"+revoked.ID.String(), later), invite.ErrRevoked)
}

func TestPg_List(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// One shared tenant/email pair unique to this run.
	base := mkInvite(t)
	tenant, email := base.Tenant, base.Email

	a := base
	b := mkInvite(t)
	b.Tenant, b.Email = tenant, email
	expired := mkInvite(t)
	expired.Tenant = tenant
	expired.ExpiresAt = now.Add(-time.Minute)
	otherTenant := mkInvite(t)
	for _, inv := range []invite.Invite{a, b, expired, otherTenant} {
		require.NoError(t, s.Create(ctx, inv))
	}
	require.NoError(t, s.Accept(ctx, b.ID, now))

	// Ordering is unasserted: same-millisecond UUIDv7 ids have random tails.
	all, err := s.List(ctx, invite.Filter{Tenant: tenant})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.ElementsMatch(t, []id.UUID{a.ID, b.ID, expired.ID}, []id.UUID{all[0].ID, all[1].ID, all[2].ID})

	byEmail, err := s.List(ctx, invite.Filter{Tenant: tenant, Email: email})
	require.NoError(t, err)
	assert.Len(t, byEmail, 2)

	// Pending excludes the accepted and the expired record.
	pending, err := s.List(ctx, invite.Filter{Tenant: tenant, Pending: true})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, a.ID, pending[0].ID)

	// Unscoped pending-only listing (the forge_invites_pending_idx shape):
	// contains-style asserts because the table is shared across tests/runs.
	allPending, err := s.List(ctx, invite.Filter{Pending: true})
	require.NoError(t, err)
	ids := make(map[id.UUID]bool, len(allPending))
	for _, inv := range allPending {
		ids[inv.ID] = true
	}
	assert.True(t, ids[a.ID], "pending invite missing from unscoped listing")
	assert.False(t, ids[b.ID], "accepted invite leaked into pending listing")
	assert.False(t, ids[expired.ID], "expired invite leaked into pending listing")

	// Empty result is non-nil, matching the memory store.
	empty, err := s.List(ctx, invite.Filter{Tenant: "absent-" + base.ID.String()})
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
}

func TestPg_ManagerEndToEnd(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mgr := invite.New(s)

	uid := id.NewUUID()
	email := "e2e-" + uid.String() + "@example.com"
	inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: email, Tenant: "t-" + uid.String(), Role: "admin"})
	require.NoError(t, err)

	// Resend rotates: old token dead, new one accepts once.
	_, rotated, err := mgr.Resend(ctx, inv.ID)
	require.NoError(t, err)
	_, err = mgr.Accept(ctx, plaintext)
	assert.ErrorIs(t, err, invite.ErrInviteNotFound)

	claim, err := mgr.Accept(ctx, rotated)
	require.NoError(t, err)
	assert.Equal(t, email, claim.Email)
	assert.Equal(t, "admin", claim.Role)
	_, err = mgr.Accept(ctx, rotated)
	assert.ErrorIs(t, err, invite.ErrAlreadyAccepted)
}
