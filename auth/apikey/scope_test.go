package apikey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

type tenantCtxKey struct{}

func tenantCtx(tenant string) context.Context {
	return context.WithValue(context.Background(), tenantCtxKey{}, tenant)
}

// scopedManager derives the tenant from context, as a multi-tenant
// application wires it.
func scopedManager(t *testing.T) *apikey.Manager {
	t.Helper()
	return mustManager(t, apikey.WithScope(func(ctx context.Context) (string, error) {
		tenant, _ := ctx.Value(tenantCtxKey{}).(string)
		return tenant, nil
	}))
}

func TestScope_CreateStampsResolvedTenant(t *testing.T) {
	t.Parallel()
	var stored apikey.Key
	_, _, err := expose(scopedManager(t).Create(tenantCtx("t1"),
		apikey.CreateParams{Subject: "u1"}, captureKey(&stored)))
	require.NoError(t, err)
	assert.Equal(t, "t1", stored.Tenant)
}

func TestScope_CreateAcceptsMatchingExplicitTenant(t *testing.T) {
	t.Parallel()
	_, _, err := expose(scopedManager(t).Create(tenantCtx("t1"),
		apikey.CreateParams{Subject: "u1", Tenant: "t1"}, discardKey))
	assert.NoError(t, err)
}

func TestScope_CreateRejectsConflictingExplicitTenant(t *testing.T) {
	t.Parallel()
	_, _, err := expose(scopedManager(t).Create(tenantCtx("t1"),
		apikey.CreateParams{Subject: "u1", Tenant: "t2"}, discardKey))
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

func TestScope_FailsClosedOnEmptyTenant(t *testing.T) {
	t.Parallel()
	mgr := scopedManager(t)
	ctx := context.Background()

	t.Run("Create", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(mgr.Create(ctx, apikey.CreateParams{Subject: "u1"}, discardKey))
		assert.ErrorIs(t, err, apikey.ErrScope)
	})

	t.Run("List", func(t *testing.T) {
		t.Parallel()
		_, err := mgr.List(ctx, apikey.Filter{}, listsNothing)
		assert.ErrorIs(t, err, apikey.ErrScope)
	})

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		_, err := mgr.Get(ctx, id.UUID{15: 1}, loadsKey(apikey.Key{}))
		assert.ErrorIs(t, err, apikey.ErrScope)
	})
}

func TestScope_FailsClosedOnHookError(t *testing.T) {
	t.Parallel()
	errHook := errors.New("tenant lookup failed")
	mgr := mustManager(t, apikey.WithScope(func(context.Context) (string, error) { return "", errHook }))

	_, _, err := expose(mgr.Create(context.Background(),
		apikey.CreateParams{Subject: "u1"}, discardKey))
	assert.ErrorIs(t, err, apikey.ErrScope)
	assert.ErrorIs(t, err, errHook)
}

// TestScope_FailsClosedBeforeWriting pins that a refused scope never
// reaches the caller's storage effect.
func TestScope_FailsClosedBeforeWriting(t *testing.T) {
	t.Parallel()
	saved := false

	_, _, err := expose(scopedManager(t).Create(context.Background(),
		apikey.CreateParams{Subject: "u1"},
		func(context.Context, apikey.Key) error {
			saved = true
			return nil
		}))
	require.ErrorIs(t, err, apikey.ErrScope)
	assert.False(t, saved)
}

func TestScope_ForeignTenantRecordReadsAsNotFound(t *testing.T) {
	t.Parallel()
	mgr := scopedManager(t)
	foreign := apikey.Key{ID: id.UUID{15: 1}, Subject: "u1", Tenant: "t1"}

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		_, err := mgr.Get(tenantCtx("t2"), foreign.ID, loadsKey(foreign))
		assert.ErrorIs(t, err, apikey.ErrNotFound)
	})

	t.Run("Revoke", func(t *testing.T) {
		t.Parallel()
		err := mgr.Revoke(tenantCtx("t2"), foreign.ID, loadsKey(foreign), discardStamp)
		assert.ErrorIs(t, err, apikey.ErrNotFound)
	})

	t.Run("Rotate", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(mgr.Rotate(tenantCtx("t2"), foreign.ID, time.Hour,
			loadsKey(foreign), discardSwap))
		assert.ErrorIs(t, err, apikey.ErrNotFound)
	})
}

func TestScope_OwningTenantReadsItsOwnRecord(t *testing.T) {
	t.Parallel()
	own := apikey.Key{ID: id.UUID{15: 1}, Subject: "u1", Tenant: "t1"}

	got, err := scopedManager(t).Get(tenantCtx("t1"), own.ID, loadsKey(own))
	require.NoError(t, err)
	assert.Equal(t, own.ID, got.ID)
}

func TestScope_ListIsConfinedToResolvedTenant(t *testing.T) {
	t.Parallel()
	var asked apikey.Filter

	_, err := scopedManager(t).List(tenantCtx("t1"), apikey.Filter{Subject: "u1"},
		func(_ context.Context, f apikey.Filter) ([]apikey.Key, error) {
			asked = f
			return nil, nil
		})
	require.NoError(t, err)
	assert.Equal(t, "t1", asked.Tenant)
}

func TestScope_ListRejectsConflictingFilterTenant(t *testing.T) {
	t.Parallel()
	_, err := scopedManager(t).List(tenantCtx("t1"), apikey.Filter{Tenant: "t2"}, listsNothing)
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

// TestScope_RotateKeepsOwningTenant pins that the replacement inherits the
// old key's tenant instead of resolving the hook a second time.
func TestScope_RotateKeepsOwningTenant(t *testing.T) {
	t.Parallel()
	old := rotatable()

	fresh, _, err := expose(scopedManager(t).Rotate(tenantCtx("t1"), old.ID, time.Hour,
		loadsKey(old), discardSwap))
	require.NoError(t, err)
	assert.Equal(t, "t1", fresh.Tenant)
}

// TestScope_UnscopedManagerPassesTenantThrough pins the single-tenant path:
// no hook means the caller's own tenant value is used verbatim.
func TestScope_UnscopedManagerPassesTenantThrough(t *testing.T) {
	t.Parallel()
	var stored apikey.Key
	_, _, err := expose(mustManager(t).Create(context.Background(),
		apikey.CreateParams{Subject: "u1", Tenant: "whatever"}, captureKey(&stored)))
	require.NoError(t, err)
	assert.Equal(t, "whatever", stored.Tenant)
}
