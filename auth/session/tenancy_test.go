package session_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/ctxkey"
)

// The tests below are executable proof that the single WithScope construction
// seam serves the three tenancy shapes a real application needs: a
// single-tenant app, a white-label app where every user is locked to one
// tenant, and a platform where a global user switches between tenants. The
// scope hook is arbitrary app logic mapping the request context to a scope
// string, so "switching tenants" is nothing more than a different context per
// request.

var tenantKey = ctxkey.New[string]("tenant")

func tenantScope(ctx context.Context) (string, error) {
	tenant, ok := tenantKey.From(ctx)
	if !ok {
		return "", errors.New("no tenant in context")
	}
	return tenant, nil
}

func tenantCtx(t *testing.T, tenant string) context.Context {
	t.Helper()
	return tenantKey.With(t.Context(), tenant)
}

// TestTenancy_SingleTenant: omit WithScope entirely. Scope resolves to "" and
// every session shares one flat namespace — zero ceremony.
func TestTenancy_SingleTenant(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	s := mgr.Start(t.Context())
	require.NoError(t, mgr.Save(t.Context(), s))
	_, err := mgr.Load(t.Context(), s.Token)
	require.NoError(t, err)
}

// TestTenancy_WhiteLabelTenantLocked: every request is bound to exactly one
// tenant. A session saved under tenant A is invisible under tenant B, and a
// request arriving without a tenant fails closed rather than leaking into a
// shared bucket.
func TestTenancy_WhiteLabelTenantLocked(t *testing.T) {
	t.Parallel()
	store := session.NewMemoryStore()
	mgr, err := session.New[data](store, session.WithScope(tenantScope))
	require.NoError(t, err)

	ctxA, ctxB := tenantCtx(t, "tenant-a"), tenantCtx(t, "tenant-b")

	s := mgr.Start(ctxA)
	require.NoError(t, mgr.Authenticate(ctxA, s, "user-1"))

	// The token is a valid credential — but only inside its own tenant.
	_, err = mgr.Load(ctxA, s.Token)
	require.NoError(t, err)
	_, err = mgr.Load(ctxB, s.Token)
	assert.ErrorIs(t, err, session.ErrNotFound, "cross-tenant token must be indistinguishable from unknown")

	// And tenant B must not be able to revoke it either.
	sb := *s
	assert.ErrorIs(t, mgr.Destroy(ctxB, &sb), session.ErrNotFound)
	_, err = mgr.Load(ctxA, s.Token)
	require.NoError(t, err, "a cross-tenant probe must not destroy the session")

	// Every operation fails closed without a tenant in context.
	_, err = mgr.Load(t.Context(), s.Token)
	assert.ErrorIs(t, err, session.ErrScope)
	assert.ErrorIs(t, mgr.Save(t.Context(), s), session.ErrScope)
	assert.ErrorIs(t, mgr.Rotate(t.Context(), s), session.ErrScope)
	assert.ErrorIs(t, mgr.Destroy(t.Context(), s), session.ErrScope)
	_, err = mgr.ListUserSessions(t.Context(), "user-1")
	assert.ErrorIs(t, err, session.ErrScope)
	assert.ErrorIs(t, mgr.DeleteUserSessions(t.Context(), "user-1"), session.ErrScope)

	// An erroring hook fails closed too. Destroy is the case that matters
	// most: it must not delete another scope's record.
	empty, err := session.New[data](store, session.WithScope(func(context.Context) (string, error) {
		return "", nil
	}))
	require.NoError(t, err)
	_, err = empty.Load(t.Context(), s.Token)
	assert.ErrorIs(t, err, session.ErrScope)
}

// TestTenancy_PlatformUserSwitchesTenants: the same user id holds separate
// sessions in different tenants; per-tenant listing and deletion never bleed
// across.
func TestTenancy_PlatformUserSwitchesTenants(t *testing.T) {
	t.Parallel()
	mgr, err := session.New[data](session.NewMemoryStore(), session.WithScope(tenantScope))
	require.NoError(t, err)

	ctxA, ctxB := tenantCtx(t, "tenant-a"), tenantCtx(t, "tenant-b")

	sa := mgr.Start(ctxA)
	require.NoError(t, mgr.Authenticate(ctxA, sa, "user-1"))
	sb := mgr.Start(ctxB)
	require.NoError(t, mgr.Authenticate(ctxB, sb, "user-1"))

	listA, err := mgr.ListUserSessions(ctxA, "user-1")
	require.NoError(t, err)
	require.Len(t, listA, 1)
	assert.Equal(t, sa.ID, listA[0].ID)

	// GDPR-deleting the user inside tenant A leaves tenant B intact.
	require.NoError(t, mgr.DeleteUserSessions(ctxA, "user-1"))
	_, err = mgr.Load(ctxA, sa.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
	_, err = mgr.Load(ctxB, sb.Token)
	require.NoError(t, err)
}
