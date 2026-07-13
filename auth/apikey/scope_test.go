package apikey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
)

type tenantCtxKey struct{}

func scopeHook(ctx context.Context) (string, error) {
	t, _ := ctx.Value(tenantCtxKey{}).(string)
	return t, nil
}

func tenantCtx(tenant string) context.Context {
	return context.WithValue(context.Background(), tenantCtxKey{}, tenant)
}

func newScoped(t *testing.T) *apikey.Manager {
	t.Helper()
	return apikey.New(apikey.NewMemoryStore(), apikey.WithScope(scopeHook))
}

func TestScope_CreateStampsTenant(t *testing.T) {
	t.Parallel()
	mgr := newScoped(t)

	k, _, err := mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	assert.Equal(t, "t1", k.Tenant)

	// Matching explicit tenant is fine; conflicting one is not.
	_, _, err = mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1", Tenant: "t1"})
	assert.NoError(t, err)
	_, _, err = mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1", Tenant: "t2"})
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

func TestScope_FailClosed(t *testing.T) {
	t.Parallel()
	// Empty tenant from the hook.
	mgr := newScoped(t)
	_, _, err := mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	assert.ErrorIs(t, err, apikey.ErrScope)
	_, err = mgr.List(context.Background(), apikey.Filter{})
	assert.ErrorIs(t, err, apikey.ErrScope)

	// Hook error.
	boom := errors.New("boom")
	failing := apikey.New(apikey.NewMemoryStore(), apikey.WithScope(
		func(context.Context) (string, error) { return "", boom }))
	_, _, err = failing.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	assert.ErrorIs(t, err, apikey.ErrScope)
	assert.ErrorIs(t, err, boom)
}

func TestScope_CrossTenantReadsAsNotFound(t *testing.T) {
	t.Parallel()
	mgr := newScoped(t)
	k, _, err := mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	_, err = mgr.Get(tenantCtx("t2"), k.ID)
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.ErrorIs(t, mgr.Revoke(tenantCtx("t2"), k.ID), apikey.ErrNotFound)
	_, _, err = mgr.Rotate(tenantCtx("t2"), k.ID, time.Hour)
	assert.ErrorIs(t, err, apikey.ErrNotFound)

	// Same tenant still works.
	_, err = mgr.Get(tenantCtx("t1"), k.ID)
	assert.NoError(t, err)
}

func TestScope_ListConfined(t *testing.T) {
	t.Parallel()
	mgr := newScoped(t)
	_, _, err := mgr.Create(tenantCtx("t1"), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)
	_, _, err = mgr.Create(tenantCtx("t2"), apikey.CreateParams{Subject: "u2"})
	require.NoError(t, err)

	keys, err := mgr.List(tenantCtx("t1"), apikey.Filter{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "t1", keys[0].Tenant)

	// A conflicting explicit filter tenant is rejected, not silently overridden.
	_, err = mgr.List(tenantCtx("t1"), apikey.Filter{Tenant: "t2"})
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}
