package shortlink_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/shortlink"
)

type tenantKey struct{}

func scopeHook(ctx context.Context) (string, error) {
	t, _ := ctx.Value(tenantKey{}).(string)
	return t, nil
}

func tenantCtx(tenant string) context.Context {
	return context.WithValue(context.Background(), tenantKey{}, tenant)
}

func newScoped(store shortlink.Store) *shortlink.Manager {
	return shortlink.New(store, shortlink.WithScope(scopeHook))
}

func TestScope_CreateStampsTenant(t *testing.T) {
	t.Parallel()
	mgr := newScoped(shortlink.NewMemoryStore())

	l, err := mgr.Create(tenantCtx("t1"), shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)
	assert.Equal(t, "t1", l.Tenant)

	// Matching explicit tenant passes; conflicting one is rejected.
	_, err = mgr.Create(tenantCtx("t1"), shortlink.CreateParams{URL: "https://example.com", Tenant: "t1"})
	assert.NoError(t, err)
	_, err = mgr.Create(tenantCtx("t1"), shortlink.CreateParams{URL: "https://example.com", Tenant: "t2"})
	assert.ErrorIs(t, err, shortlink.ErrTenantMismatch)
}

func TestScope_FailClosed(t *testing.T) {
	t.Parallel()
	mgr := newScoped(shortlink.NewMemoryStore())

	// Empty tenant from the hook fails every management operation.
	ctx := context.Background()
	_, err := mgr.Create(ctx, shortlink.CreateParams{URL: "https://example.com"})
	assert.ErrorIs(t, err, shortlink.ErrScope)
	_, err = mgr.Get(ctx, "x")
	assert.ErrorIs(t, err, shortlink.ErrScope)
	_, err = mgr.List(ctx, shortlink.Filter{})
	assert.ErrorIs(t, err, shortlink.ErrScope)
	assert.ErrorIs(t, mgr.Deactivate(ctx, "x"), shortlink.ErrScope)
	assert.ErrorIs(t, mgr.Activate(ctx, "x"), shortlink.ErrScope)
	assert.ErrorIs(t, mgr.Delete(ctx, "x"), shortlink.ErrScope)

	hookErr := errors.New("boom")
	failing := shortlink.New(shortlink.NewMemoryStore(), shortlink.WithScope(
		func(context.Context) (string, error) { return "", hookErr },
	))
	_, err = failing.Create(context.Background(), shortlink.CreateParams{URL: "https://example.com"})
	assert.ErrorIs(t, err, shortlink.ErrScope)
	assert.ErrorIs(t, err, hookErr)
}

func TestScope_CrossTenantHidden(t *testing.T) {
	t.Parallel()
	store := shortlink.NewMemoryStore()
	mgr := newScoped(store)

	l, err := mgr.Create(tenantCtx("t1"), shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	// Another tenant cannot see or mutate the link.
	_, err = mgr.Get(tenantCtx("t2"), l.Code)
	assert.ErrorIs(t, err, shortlink.ErrNotFound)
	assert.ErrorIs(t, mgr.Deactivate(tenantCtx("t2"), l.Code), shortlink.ErrNotFound)
	assert.ErrorIs(t, mgr.Activate(tenantCtx("t2"), l.Code), shortlink.ErrNotFound)
	assert.ErrorIs(t, mgr.Delete(tenantCtx("t2"), l.Code), shortlink.ErrNotFound)

	// The owner still can.
	_, err = mgr.Get(tenantCtx("t1"), l.Code)
	assert.NoError(t, err)
}

func TestScope_ListConfined(t *testing.T) {
	t.Parallel()
	mgr := newScoped(shortlink.NewMemoryStore())

	_, err := mgr.Create(tenantCtx("t1"), shortlink.CreateParams{URL: "https://example.com/a"})
	require.NoError(t, err)
	_, err = mgr.Create(tenantCtx("t2"), shortlink.CreateParams{URL: "https://example.com/b"})
	require.NoError(t, err)

	links, err := mgr.List(tenantCtx("t1"), shortlink.Filter{})
	require.NoError(t, err)
	require.Len(t, links, 1)
	assert.Equal(t, "t1", links[0].Tenant)

	// An explicit conflicting filter tenant is rejected, not silently widened.
	_, err = mgr.List(tenantCtx("t1"), shortlink.Filter{Tenant: "t2"})
	assert.ErrorIs(t, err, shortlink.ErrTenantMismatch)
}

func TestScope_ResolveUnaffected(t *testing.T) {
	t.Parallel()
	mgr := newScoped(shortlink.NewMemoryStore())

	l, err := mgr.Create(tenantCtx("t1"), shortlink.CreateParams{URL: "https://example.com"})
	require.NoError(t, err)

	// Resolve works with no tenant in context — short codes are public.
	got, err := mgr.Resolve(context.Background(), l.Code)
	require.NoError(t, err)
	assert.Equal(t, l.URL, got.URL)
}
