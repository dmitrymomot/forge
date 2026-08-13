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

func newScoped(t *testing.T) (apikey.Config, *apikey.MemoryStore) {
	t.Helper()
	return mustConfig(t, apikey.WithScope(scopeHook)), apikey.NewMemoryStore()
}

func TestScope_CreateStampsTenant(t *testing.T) {
	t.Parallel()
	cfg, mem := newScoped(t)

	k, _, err := apikey.Create(tenantCtx("t1"), cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)
	assert.Equal(t, "t1", k.Tenant)

	_, _, err = apikey.Create(tenantCtx("t1"), cfg, apikey.CreateParams{Subject: "u1", Tenant: "t1"}, mem.Save)
	assert.NoError(t, err)
	_, _, err = apikey.Create(tenantCtx("t1"), cfg, apikey.CreateParams{Subject: "u1", Tenant: "t2"}, mem.Save)
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

func TestScope_FailClosed(t *testing.T) {
	t.Parallel()
	cfg, mem := newScoped(t)

	_, _, err := apikey.Create(context.Background(), cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	assert.ErrorIs(t, err, apikey.ErrScope)
	_, err = apikey.List(context.Background(), cfg, apikey.Filter{}, mem.List)
	assert.ErrorIs(t, err, apikey.ErrScope)

	boom := errors.New("boom")
	failing := mustConfig(t, apikey.WithScope(func(context.Context) (string, error) { return "", boom }))
	_, _, err = apikey.Create(context.Background(), failing, apikey.CreateParams{Subject: "u1"}, mem.Save)
	assert.ErrorIs(t, err, apikey.ErrScope)
	assert.ErrorIs(t, err, boom)
}

func TestScope_CrossTenantReadsAsNotFound(t *testing.T) {
	t.Parallel()
	cfg, mem := newScoped(t)
	k, _, err := apikey.Create(tenantCtx("t1"), cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)

	_, err = apikey.Get(tenantCtx("t2"), cfg, k.ID, mem.Load)
	assert.ErrorIs(t, err, apikey.ErrNotFound)
	assert.ErrorIs(t, apikey.Revoke(tenantCtx("t2"), cfg, k.ID, mem.Load, mem.Revoke), apikey.ErrNotFound)
	_, _, err = apikey.Rotate(tenantCtx("t2"), cfg, k.ID, time.Hour, mem.Load, mem.Swap)
	assert.ErrorIs(t, err, apikey.ErrNotFound)

	_, err = apikey.Get(tenantCtx("t1"), cfg, k.ID, mem.Load)
	assert.NoError(t, err)
}

func TestScope_ListConfined(t *testing.T) {
	t.Parallel()
	cfg, mem := newScoped(t)
	_, _, err := apikey.Create(tenantCtx("t1"), cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)
	_, _, err = apikey.Create(tenantCtx("t2"), cfg, apikey.CreateParams{Subject: "u2"}, mem.Save)
	require.NoError(t, err)

	keys, err := apikey.List(tenantCtx("t1"), cfg, apikey.Filter{}, mem.List)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "t1", keys[0].Tenant)

	_, err = apikey.List(tenantCtx("t1"), cfg, apikey.Filter{Tenant: "t2"}, mem.List)
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

// TestScope_RotateKeepsOwningTenant pins that a rotation performed under a
// scoped Config re-stamps the replacement with the old key's tenant rather
// than resolving the hook a second time.
func TestScope_RotateKeepsOwningTenant(t *testing.T) {
	t.Parallel()
	cfg, mem := newScoped(t)
	old, _, err := apikey.Create(tenantCtx("t1"), cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)

	fresh, _, err := apikey.Rotate(tenantCtx("t1"), cfg, old.ID, time.Hour, mem.Load, mem.Swap)
	require.NoError(t, err)
	assert.Equal(t, "t1", fresh.Tenant)
}
