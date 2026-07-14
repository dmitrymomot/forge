package rbac_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/rbac"
)

func TestManagerAssignRolesForUnassign(t *testing.T) {
	m := rbac.NewManager(rbac.NewMemoryStore())
	ctx := context.Background()

	require.NoError(t, m.Assign(ctx, "u1", "editor", "auditor"))
	require.NoError(t, m.Assign(ctx, "u1", "editor")) // idempotent

	roles, err := m.RolesFor(ctx, "u1")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"editor", "auditor"}, roles)

	require.NoError(t, m.Unassign(ctx, "u1", "auditor"))
	roles, err = m.RolesFor(ctx, "u1")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"editor"}, roles)
}

func TestManagerUnassignAllReclaims(t *testing.T) {
	m := rbac.NewManager(rbac.NewMemoryStore())
	ctx := context.Background()

	require.NoError(t, m.Assign(ctx, "u1", "editor"))
	require.NoError(t, m.Unassign(ctx, "u1", "editor")) // empties the role set

	roles, err := m.RolesFor(ctx, "u1")
	require.NoError(t, err)
	assert.Empty(t, roles)
}

func TestManagerScopeIsolation(t *testing.T) {
	store := rbac.NewMemoryStore()
	tenant := "acme"
	m := rbac.NewManager(store, rbac.WithScope(func(context.Context) (string, error) {
		return tenant, nil
	}))
	ctx := context.Background()
	require.NoError(t, m.Assign(ctx, "u1", "editor"))

	// another tenant sees nothing
	tenant = "other"
	roles, err := m.RolesFor(ctx, "u1")
	require.NoError(t, err)
	assert.Empty(t, roles)
}

func TestManagerScopeFailClosed(t *testing.T) {
	m := rbac.NewManager(rbac.NewMemoryStore(), rbac.WithScope(func(context.Context) (string, error) {
		return "", errors.New("no tenant in context")
	}))
	err := m.Assign(context.Background(), "u1", "editor")
	assert.ErrorIs(t, err, rbac.ErrScope)

	m2 := rbac.NewManager(rbac.NewMemoryStore(), rbac.WithScope(func(context.Context) (string, error) {
		return "", nil // empty tenant -> fail closed
	}))
	assert.ErrorIs(t, m2.Assign(context.Background(), "u1", "editor"), rbac.ErrScope)
}
