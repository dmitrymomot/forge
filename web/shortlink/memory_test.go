package shortlink_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/shortlink"
)

func mkLink(code, tenant string, createdAt time.Time) shortlink.Link {
	return shortlink.Link{
		Code:      code,
		URL:       "https://example.com/" + code,
		Tenant:    tenant,
		CreatedAt: createdAt,
	}
}

func TestMemoryStore_CreateDuplicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := shortlink.NewMemoryStore()

	require.NoError(t, s.Create(ctx, mkLink("abc", "", time.Now())))
	assert.ErrorIs(t, s.Create(ctx, mkLink("abc", "", time.Now())), shortlink.ErrDuplicate)
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	t.Parallel()
	s := shortlink.NewMemoryStore()
	_, err := s.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, shortlink.ErrNotFound)
}

func TestMemoryStore_ListOrderAndFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := shortlink.NewMemoryStore()
	base := time.Now().UTC()

	require.NoError(t, s.Create(ctx, mkLink("old", "t1", base.Add(-2*time.Hour))))
	require.NoError(t, s.Create(ctx, mkLink("new", "t1", base)))
	require.NoError(t, s.Create(ctx, mkLink("mid", "t2", base.Add(-time.Hour))))
	// Same timestamp as "new": ties break by code ascending.
	require.NoError(t, s.Create(ctx, mkLink("aaa", "t1", base)))

	all, err := s.List(ctx, shortlink.Filter{})
	require.NoError(t, err)
	codes := make([]string, len(all))
	for i, l := range all {
		codes[i] = l.Code
	}
	assert.Equal(t, []string{"aaa", "new", "mid", "old"}, codes)

	t2, err := s.List(ctx, shortlink.Filter{Tenant: "t2"})
	require.NoError(t, err)
	require.Len(t, t2, 1)
	assert.Equal(t, "mid", t2[0].Code)

	none, err := s.List(ctx, shortlink.Filter{Tenant: "t3"})
	require.NoError(t, err)
	assert.Empty(t, none)
	assert.NotNil(t, none)
}

func TestMemoryStore_DeactivateActivate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := shortlink.NewMemoryStore()
	require.NoError(t, s.Create(ctx, mkLink("abc", "", time.Now())))

	at := time.Now().UTC()
	require.NoError(t, s.Deactivate(ctx, "abc", "", at))
	l, err := s.Get(ctx, "abc")
	require.NoError(t, err)
	assert.Equal(t, at, l.DeactivatedAt)

	require.NoError(t, s.Activate(ctx, "abc", ""))
	l, err = s.Get(ctx, "abc")
	require.NoError(t, err)
	assert.True(t, l.DeactivatedAt.IsZero())

	assert.ErrorIs(t, s.Deactivate(ctx, "nope", "", at), shortlink.ErrNotFound)
	assert.ErrorIs(t, s.Activate(ctx, "nope", ""), shortlink.ErrNotFound)
}

func TestMemoryStore_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := shortlink.NewMemoryStore()
	require.NoError(t, s.Create(ctx, mkLink("abc", "", time.Now())))

	require.NoError(t, s.Delete(ctx, "abc", ""))
	_, err := s.Get(ctx, "abc")
	assert.ErrorIs(t, err, shortlink.ErrNotFound)
	assert.ErrorIs(t, s.Delete(ctx, "abc", ""), shortlink.ErrNotFound)
}

func TestMemoryStore_TenantPredicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := shortlink.NewMemoryStore()
	require.NoError(t, s.Create(ctx, mkLink("abc", "t1", time.Now())))

	at := time.Now().UTC()
	// A mismatched tenant is atomically rejected as ErrNotFound.
	assert.ErrorIs(t, s.Deactivate(ctx, "abc", "t2", at), shortlink.ErrNotFound)
	assert.ErrorIs(t, s.Activate(ctx, "abc", "t2"), shortlink.ErrNotFound)
	assert.ErrorIs(t, s.Delete(ctx, "abc", "t2"), shortlink.ErrNotFound)
	l, err := s.Get(ctx, "abc")
	require.NoError(t, err)
	assert.True(t, l.DeactivatedAt.IsZero())

	// The owning tenant (and the unconstrained empty tenant) pass.
	require.NoError(t, s.Deactivate(ctx, "abc", "t1", at))
	require.NoError(t, s.Activate(ctx, "abc", ""))
	require.NoError(t, s.Delete(ctx, "abc", "t1"))
}

func TestMemoryStore_ListLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := shortlink.NewMemoryStore()
	base := time.Now().UTC()
	require.NoError(t, s.Create(ctx, mkLink("a", "", base.Add(-2*time.Hour))))
	require.NoError(t, s.Create(ctx, mkLink("b", "", base.Add(-time.Hour))))
	require.NoError(t, s.Create(ctx, mkLink("c", "", base)))

	got, err := s.List(ctx, shortlink.Filter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "c", got[0].Code)
	assert.Equal(t, "b", got[1].Code)
}
