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

// scopedConfig derives the tenant from context, as a multi-tenant
// application wires it.
func scopedConfig(t *testing.T) apikey.Config {
	t.Helper()
	return mustConfig(t, apikey.WithScope(func(ctx context.Context) (string, error) {
		tenant, _ := ctx.Value(tenantCtxKey{}).(string)
		return tenant, nil
	}))
}

func TestScope_CreateStampsResolvedTenant(t *testing.T) {
	t.Parallel()
	var stored apikey.Key
	_, _, err := expose(apikey.Create(tenantCtx("t1"), scopedConfig(t),
		apikey.CreateParams{Subject: "u1"}, captureKey(&stored)))
	require.NoError(t, err)
	assert.Equal(t, "t1", stored.Tenant)
}

func TestScope_CreateAcceptsMatchingExplicitTenant(t *testing.T) {
	t.Parallel()
	_, _, err := expose(apikey.Create(tenantCtx("t1"), scopedConfig(t),
		apikey.CreateParams{Subject: "u1", Tenant: "t1"}, discardKey))
	assert.NoError(t, err)
}

func TestScope_CreateRejectsConflictingExplicitTenant(t *testing.T) {
	t.Parallel()
	_, _, err := expose(apikey.Create(tenantCtx("t1"), scopedConfig(t),
		apikey.CreateParams{Subject: "u1", Tenant: "t2"}, discardKey))
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

func TestScope_FailsClosedOnEmptyTenant(t *testing.T) {
	t.Parallel()
	cfg := scopedConfig(t)
	ctx := context.Background()

	t.Run("Create", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, discardKey))
		assert.ErrorIs(t, err, apikey.ErrScope)
	})

	t.Run("List", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.List(ctx, cfg, apikey.Filter{}, listsNothing)
		assert.ErrorIs(t, err, apikey.ErrScope)
	})

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.Get(ctx, cfg, id.UUID{15: 1}, loadsKey(apikey.Key{}))
		assert.ErrorIs(t, err, apikey.ErrScope)
	})
}

func TestScope_FailsClosedOnHookError(t *testing.T) {
	t.Parallel()
	errHook := errors.New("tenant lookup failed")
	cfg := mustConfig(t, apikey.WithScope(func(context.Context) (string, error) { return "", errHook }))

	_, _, err := expose(apikey.Create(context.Background(), cfg,
		apikey.CreateParams{Subject: "u1"}, discardKey))
	assert.ErrorIs(t, err, apikey.ErrScope)
	assert.ErrorIs(t, err, errHook)
}

// TestScope_FailsClosedBeforeWriting pins that a refused scope never
// reaches the caller's storage effect.
func TestScope_FailsClosedBeforeWriting(t *testing.T) {
	t.Parallel()
	saved := false

	_, _, err := expose(apikey.Create(context.Background(), scopedConfig(t),
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
	cfg := scopedConfig(t)
	foreign := apikey.Key{ID: id.UUID{15: 1}, Subject: "u1", Tenant: "t1"}

	t.Run("Get", func(t *testing.T) {
		t.Parallel()
		_, err := apikey.Get(tenantCtx("t2"), cfg, foreign.ID, loadsKey(foreign))
		assert.ErrorIs(t, err, apikey.ErrNotFound)
	})

	t.Run("Revoke", func(t *testing.T) {
		t.Parallel()
		err := apikey.Revoke(tenantCtx("t2"), cfg, foreign.ID, loadsKey(foreign), discardStamp)
		assert.ErrorIs(t, err, apikey.ErrNotFound)
	})

	t.Run("Rotate", func(t *testing.T) {
		t.Parallel()
		_, _, err := expose(apikey.Rotate(tenantCtx("t2"), cfg, foreign.ID, time.Hour,
			loadsKey(foreign), discardSwap))
		assert.ErrorIs(t, err, apikey.ErrNotFound)
	})
}

func TestScope_OwningTenantReadsItsOwnRecord(t *testing.T) {
	t.Parallel()
	own := apikey.Key{ID: id.UUID{15: 1}, Subject: "u1", Tenant: "t1"}

	got, err := apikey.Get(tenantCtx("t1"), scopedConfig(t), own.ID, loadsKey(own))
	require.NoError(t, err)
	assert.Equal(t, own.ID, got.ID)
}

func TestScope_ListIsConfinedToResolvedTenant(t *testing.T) {
	t.Parallel()
	var asked apikey.Filter

	_, err := apikey.List(tenantCtx("t1"), scopedConfig(t), apikey.Filter{Subject: "u1"},
		func(_ context.Context, f apikey.Filter) ([]apikey.Key, error) {
			asked = f
			return nil, nil
		})
	require.NoError(t, err)
	assert.Equal(t, "t1", asked.Tenant)
}

func TestScope_ListRejectsConflictingFilterTenant(t *testing.T) {
	t.Parallel()
	_, err := apikey.List(tenantCtx("t1"), scopedConfig(t), apikey.Filter{Tenant: "t2"}, listsNothing)
	assert.ErrorIs(t, err, apikey.ErrTenantMismatch)
}

// TestScope_RotateKeepsOwningTenant pins that the replacement inherits the
// old key's tenant instead of resolving the hook a second time.
func TestScope_RotateKeepsOwningTenant(t *testing.T) {
	t.Parallel()
	old := rotatable()

	fresh, _, err := expose(apikey.Rotate(tenantCtx("t1"), scopedConfig(t), old.ID, time.Hour,
		loadsKey(old), discardSwap))
	require.NoError(t, err)
	assert.Equal(t, "t1", fresh.Tenant)
}

// TestScope_UnscopedConfigPassesTenantThrough pins the single-tenant path:
// no hook means the caller's own tenant value is used verbatim.
func TestScope_UnscopedConfigPassesTenantThrough(t *testing.T) {
	t.Parallel()
	var stored apikey.Key
	_, _, err := expose(apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1", Tenant: "whatever"}, captureKey(&stored)))
	require.NoError(t, err)
	assert.Equal(t, "whatever", stored.Tenant)
}
