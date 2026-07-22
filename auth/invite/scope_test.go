package invite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/invite"
	"github.com/dmitrymomot/forge/core/id"
)

type tenantCtxKey struct{}

func scopeHook(ctx context.Context) (string, error) {
	t, _ := ctx.Value(tenantCtxKey{}).(string)
	return t, nil
}

func tenantCtx(tenant string) context.Context {
	return context.WithValue(context.Background(), tenantCtxKey{}, tenant)
}

func TestScope_CreateStampsTenant(t *testing.T) {
	t.Parallel()
	mgr := invite.New(invite.NewMemoryStore(), invite.WithScope(scopeHook))

	inv, _, err := mgr.Create(tenantCtx("t1"), invite.CreateParams{Email: "a@b.com"})
	require.NoError(t, err)
	assert.Equal(t, "t1", inv.Tenant)

	// Matching explicit tenant is fine; conflicting one is not.
	_, _, err = mgr.Create(tenantCtx("t1"), invite.CreateParams{Email: "a@b.com", Tenant: "t1"})
	assert.NoError(t, err)
	_, _, err = mgr.Create(tenantCtx("t1"), invite.CreateParams{Email: "a@b.com", Tenant: "t2"})
	assert.ErrorIs(t, err, invite.ErrTenantMismatch)
}

// TestScope_FailClosed sweeps every scoped operation: with the hook
// yielding no tenant (empty) or an error, each must refuse with ErrScope
// rather than fall back to unscoped behavior.
func TestScope_FailClosed(t *testing.T) {
	t.Parallel()
	hookErr := errors.New("no session")
	for name, hook := range map[string]func(context.Context) (string, error){
		"empty tenant": scopeHook, // background ctx carries no tenant
		"hook error":   func(context.Context) (string, error) { return "", hookErr },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mgr := invite.New(invite.NewMemoryStore(), invite.WithScope(hook))
			ctx := context.Background()

			_, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
			assert.ErrorIs(t, err, invite.ErrScope)
			_, err = mgr.Get(ctx, id.NewUUID())
			assert.ErrorIs(t, err, invite.ErrScope)
			_, err = mgr.List(ctx, invite.Filter{})
			assert.ErrorIs(t, err, invite.ErrScope)
			err = mgr.Revoke(ctx, id.NewUUID())
			assert.ErrorIs(t, err, invite.ErrScope)
			_, _, err = mgr.Resend(ctx, id.NewUUID())
			assert.ErrorIs(t, err, invite.ErrScope)
		})
	}
}

func TestScope_CrossTenantReadsAsNotFound(t *testing.T) {
	t.Parallel()
	store := invite.NewMemoryStore()
	mgr := invite.New(store, invite.WithScope(scopeHook))

	inv, _, err := mgr.Create(tenantCtx("t1"), invite.CreateParams{Email: "a@b.com"})
	require.NoError(t, err)

	// Same record, other tenant's context: invisible for every management op.
	_, err = mgr.Get(tenantCtx("t2"), inv.ID)
	assert.ErrorIs(t, err, invite.ErrNotFound)
	assert.ErrorIs(t, mgr.Revoke(tenantCtx("t2"), inv.ID), invite.ErrNotFound)
	_, _, err = mgr.Resend(tenantCtx("t2"), inv.ID)
	assert.ErrorIs(t, err, invite.ErrNotFound)

	got, err := mgr.Get(tenantCtx("t1"), inv.ID)
	require.NoError(t, err)
	assert.Equal(t, inv.ID, got.ID)
}

func TestScope_ListConfined(t *testing.T) {
	t.Parallel()
	store := invite.NewMemoryStore()
	mgr := invite.New(store, invite.WithScope(scopeHook))

	_, _, err := mgr.Create(tenantCtx("t1"), invite.CreateParams{Email: "a@b.com"})
	require.NoError(t, err)
	_, _, err = mgr.Create(tenantCtx("t2"), invite.CreateParams{Email: "b@b.com"})
	require.NoError(t, err)

	got, err := mgr.List(tenantCtx("t1"), invite.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "t1", got[0].Tenant)

	// An explicit conflicting filter tenant cannot widen the scope.
	_, err = mgr.List(tenantCtx("t1"), invite.Filter{Tenant: "t2"})
	assert.ErrorIs(t, err, invite.ErrTenantMismatch)
}

func TestScope_AcceptAndPeekUnscoped(t *testing.T) {
	t.Parallel()
	store := invite.NewMemoryStore()
	mgr := invite.New(store, invite.WithScope(scopeHook))

	inv, plaintext, err := mgr.Create(tenantCtx("t1"), invite.CreateParams{Email: "a@b.com", Role: "editor"})
	require.NoError(t, err)

	// The invitee has no tenant context — background ctx must still work.
	ctx := context.Background()
	peeked, err := mgr.Peek(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, "t1", peeked.Tenant)

	claim, err := mgr.Accept(ctx, plaintext)
	require.NoError(t, err)
	assert.Equal(t, invite.Claim{Tenant: "t1", Email: "a@b.com", Role: "editor", ID: inv.ID}, claim)
}
