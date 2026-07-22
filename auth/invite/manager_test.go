package invite_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/invite"
	"github.com/dmitrymomot/forge/core/id"
)

func newManager(t *testing.T, opts ...invite.Option) *invite.Manager {
	t.Helper()
	return invite.New(invite.NewMemoryStore(), opts...)
}

func TestNew_Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { invite.New(nil) })
	assert.Panics(t, func() { invite.New(invite.NewMemoryStore(), invite.WithTTL(0)) })
	assert.Panics(t, func() { invite.New(invite.NewMemoryStore(), invite.WithTTL(-time.Hour)) })
}

func TestCreate(t *testing.T) {
	t.Parallel()
	mgr := newManager(t, invite.WithTTL(time.Hour))
	ctx := context.Background()

	before := time.Now().UTC()
	inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1", Role: "editor"})
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(plaintext, "inv_"))
	assert.NotContains(t, plaintext, inv.Hash, "plaintext must not embed the hash")
	assert.Equal(t, "a@b.com", inv.Email)
	assert.Equal(t, "t1", inv.Tenant)
	assert.Equal(t, "editor", inv.Role)
	assert.False(t, inv.ID.IsZero())
	assert.True(t, inv.Pending(time.Now().UTC()))
	assert.WithinRange(t, inv.ExpiresAt, before.Add(time.Hour), time.Now().UTC().Add(time.Hour))

	// Only the hash is persisted — the stored record matches the returned one.
	got, err := mgr.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, inv.Hash, got.Hash)
	assert.NotEqual(t, plaintext, got.Hash)
}

func TestCreate_EmailRequired(t *testing.T) {
	t.Parallel()
	mgr := newManager(t)
	_, _, err := mgr.Create(context.Background(), invite.CreateParams{})
	assert.ErrorIs(t, err, invite.ErrEmailRequired)
}

func TestAccept(t *testing.T) {
	t.Parallel()
	mgr := newManager(t)
	ctx := context.Background()
	inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1", Role: "admin"})
	require.NoError(t, err)

	claim, err := mgr.Accept(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, invite.Claim{Tenant: "t1", Email: "a@b.com", Role: "admin", ID: inv.ID}, claim)

	got, err := mgr.Get(ctx, inv.ID)
	require.NoError(t, err)
	assert.False(t, got.AcceptedAt.IsZero())
	assert.False(t, got.Pending(time.Now().UTC()))

	// Single-use: the replay reports ErrAlreadyAccepted.
	_, err = mgr.Accept(ctx, plaintext)
	assert.ErrorIs(t, err, invite.ErrAlreadyAccepted)
}

func TestAccept_Rejections(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("malformed", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t)
		for _, tok := range []string{"", "inv_", "garbage", strings.Repeat("a", 53), "inv_" + strings.Repeat("a", 49)} {
			_, err := mgr.Accept(ctx, tok)
			assert.ErrorIs(t, err, invite.ErrMalformedToken, "token %q", tok)
		}
	})

	t.Run("wellformed unknown", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t)
		_, other, err := newManager(t).Create(ctx, invite.CreateParams{Email: "a@b.com"})
		require.NoError(t, err)
		_, err = mgr.Accept(ctx, other) // valid structure, no record in this store
		assert.ErrorIs(t, err, invite.ErrInviteNotFound)
	})

	t.Run("revoked", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t)
		inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
		require.NoError(t, err)
		require.NoError(t, mgr.Revoke(ctx, inv.ID))
		_, err = mgr.Accept(ctx, plaintext)
		assert.ErrorIs(t, err, invite.ErrRevoked)
	})

	t.Run("expired", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t, invite.WithTTL(time.Nanosecond))
		_, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
		require.NoError(t, err)
		_, err = mgr.Accept(ctx, plaintext)
		assert.ErrorIs(t, err, invite.ErrExpired)
	})
}

func TestAccept_ConcurrentSingleUse(t *testing.T) {
	t.Parallel()
	mgr := newManager(t)
	ctx := context.Background()
	_, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
	require.NoError(t, err)

	const n = 32
	var wg sync.WaitGroup
	wins := make(chan invite.Claim, n)
	var start sync.WaitGroup
	start.Add(1)
	for range n {
		wg.Go(func() {
			start.Wait()
			if claim, err := mgr.Accept(ctx, plaintext); err == nil {
				wins <- claim
			} else {
				assert.ErrorIs(t, err, invite.ErrAlreadyAccepted)
			}
		})
	}
	start.Done()
	wg.Wait()
	close(wins)
	assert.Len(t, wins, 1, "exactly one concurrent accept must win")
}

func TestPeek_DoesNotConsume(t *testing.T) {
	t.Parallel()
	mgr := newManager(t)
	ctx := context.Background()
	inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1", Role: "viewer"})
	require.NoError(t, err)

	for range 3 { // scanner prefetches must not burn the invite
		claim, err := mgr.Peek(ctx, plaintext)
		require.NoError(t, err)
		assert.Equal(t, invite.Claim{Tenant: "t1", Email: "a@b.com", Role: "viewer", ID: inv.ID}, claim)
	}

	_, err = mgr.Accept(ctx, plaintext)
	require.NoError(t, err)
	_, err = mgr.Peek(ctx, plaintext)
	assert.ErrorIs(t, err, invite.ErrAlreadyAccepted)
}

func TestRevoke(t *testing.T) {
	t.Parallel()
	mgr := newManager(t)
	ctx := context.Background()

	t.Run("pending then idempotent", func(t *testing.T) {
		t.Parallel()
		inv, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
		require.NoError(t, err)
		require.NoError(t, mgr.Revoke(ctx, inv.ID))
		got, err := mgr.Get(ctx, inv.ID)
		require.NoError(t, err)
		first := got.RevokedAt
		require.False(t, first.IsZero())

		require.NoError(t, mgr.Revoke(ctx, inv.ID)) // no-op
		got, err = mgr.Get(ctx, inv.ID)
		require.NoError(t, err)
		assert.Equal(t, first, got.RevokedAt, "second revoke must keep the original timestamp")
	})

	t.Run("accepted", func(t *testing.T) {
		t.Parallel()
		inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "b@b.com"})
		require.NoError(t, err)
		_, err = mgr.Accept(ctx, plaintext)
		require.NoError(t, err)
		assert.ErrorIs(t, mgr.Revoke(ctx, inv.ID), invite.ErrAlreadyAccepted)
	})

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()
		assert.ErrorIs(t, mgr.Revoke(ctx, id.NewUUID()), invite.ErrNotFound)
	})
}

func TestResend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("rotates token and expiry", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t)
		inv, oldToken, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1", Role: "editor"})
		require.NoError(t, err)

		fresh, newToken, err := mgr.Resend(ctx, inv.ID)
		require.NoError(t, err)
		assert.NotEqual(t, oldToken, newToken)
		assert.Equal(t, inv.ID, fresh.ID)
		assert.NotEqual(t, inv.Hash, fresh.Hash)
		assert.False(t, fresh.ExpiresAt.Before(inv.ExpiresAt))

		// The old token is dead; the new one carries the same claim.
		_, err = mgr.Accept(ctx, oldToken)
		assert.ErrorIs(t, err, invite.ErrInviteNotFound)
		claim, err := mgr.Accept(ctx, newToken)
		require.NoError(t, err)
		assert.Equal(t, invite.Claim{Tenant: "t1", Email: "a@b.com", Role: "editor", ID: inv.ID}, claim)
	})

	t.Run("revives expired invite", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t, invite.WithTTL(time.Nanosecond))
		inv, stale, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
		require.NoError(t, err)
		_, err = mgr.Accept(ctx, stale)
		require.ErrorIs(t, err, invite.ErrExpired)

		// A fresh TTL applies from resend time; nanosecond TTL keeps it
		// expired, which is fine — what matters is the rotation succeeded.
		fresh, rotated, err := mgr.Resend(ctx, inv.ID)
		require.NoError(t, err)
		assert.NotEqual(t, stale, rotated)
		assert.True(t, fresh.ExpiresAt.After(inv.ExpiresAt))
	})

	t.Run("accepted", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t)
		inv, plaintext, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
		require.NoError(t, err)
		_, err = mgr.Accept(ctx, plaintext)
		require.NoError(t, err)
		_, _, err = mgr.Resend(ctx, inv.ID)
		assert.ErrorIs(t, err, invite.ErrAlreadyAccepted)
	})

	t.Run("revoked", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t)
		inv, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
		require.NoError(t, err)
		require.NoError(t, mgr.Revoke(ctx, inv.ID))
		_, _, err = mgr.Resend(ctx, inv.ID)
		assert.ErrorIs(t, err, invite.ErrRevoked)
	})

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()
		mgr := newManager(t)
		_, _, err := mgr.Resend(ctx, id.NewUUID())
		assert.ErrorIs(t, err, invite.ErrNotFound)
	})
}

func TestList(t *testing.T) {
	t.Parallel()
	mgr := newManager(t)
	ctx := context.Background()

	a, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t1"})
	require.NoError(t, err)
	b, bTok, err := mgr.Create(ctx, invite.CreateParams{Email: "b@b.com", Tenant: "t1"})
	require.NoError(t, err)
	c, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com", Tenant: "t2"})
	require.NoError(t, err)

	all, err := mgr.List(ctx, invite.Filter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Ordering is untested here: same-millisecond UUIDv7 ids have random
	// tails (store_test pins ordering with handcrafted ids).
	t1, err := mgr.List(ctx, invite.Filter{Tenant: "t1"})
	require.NoError(t, err)
	require.Len(t, t1, 2)
	assert.ElementsMatch(t, []id.UUID{a.ID, b.ID}, []id.UUID{t1[0].ID, t1[1].ID})

	byEmail, err := mgr.List(ctx, invite.Filter{Email: "a@b.com"})
	require.NoError(t, err)
	assert.Len(t, byEmail, 2)

	// Accepting b removes it from the pending view only.
	_, err = mgr.Accept(ctx, bTok)
	require.NoError(t, err)
	pending, err := mgr.List(ctx, invite.Filter{Pending: true})
	require.NoError(t, err)
	require.Len(t, pending, 2)
	assert.ElementsMatch(t, []id.UUID{a.ID, c.ID}, []id.UUID{pending[0].ID, pending[1].ID})
}
