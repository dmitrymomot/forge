package rbac_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/rbac"
)

func mkSet(t *testing.T) *rbac.RoleSet {
	t.Helper()
	rs, err := rbac.NewRoleSet(
		rbac.WithRoles(
			rbac.Role("viewer", "documents:read", "comments:read"),
			rbac.Role("editor", "documents:*"),
			rbac.Role("admin", "*"),
			rbac.Role("auditor", "auditlog:read"),
		),
		rbac.WithRoleInheritance(
			rbac.RoleInherits("editor", "viewer"),
			rbac.RoleInherits("manager", "editor", "auditor"),
		),
	)
	require.NoError(t, err)
	return rs
}

func TestCanDirectAndWildcard(t *testing.T) {
	rs := mkSet(t)
	assert.True(t, rs.Can([]string{"viewer"}, "documents:read"))
	assert.False(t, rs.Can([]string{"viewer"}, "documents:write"))
	assert.True(t, rs.Can([]string{"editor"}, "documents:write")) // wildcard
	assert.True(t, rs.Can([]string{"admin"}, "anything:here"))    // super
}

func TestCanThroughInheritance(t *testing.T) {
	rs := mkSet(t)
	// editor inherits viewer's grants
	assert.True(t, rs.Can([]string{"editor"}, "comments:read"))
	// manager inherits editor (-> viewer) and auditor
	assert.True(t, rs.Can([]string{"manager"}, "documents:write"))
	assert.True(t, rs.Can([]string{"manager"}, "auditlog:read"))
	assert.True(t, rs.Can([]string{"manager"}, "comments:read"))
}

func TestCanUnknownRoleSkipped(t *testing.T) {
	rs := mkSet(t)
	assert.False(t, rs.Can([]string{"ghost"}, "documents:read"))
	assert.True(t, rs.Can([]string{"ghost", "viewer"}, "documents:read"))
}

func TestHasRoleInheritanceAware(t *testing.T) {
	rs := mkSet(t)
	assert.True(t, rs.HasRole([]string{"editor"}, "viewer"))   // ancestor
	assert.True(t, rs.HasRole([]string{"editor"}, "editor"))   // self
	assert.True(t, rs.HasRole([]string{"manager"}, "auditor")) // multi-parent
	assert.False(t, rs.HasRole([]string{"viewer"}, "editor"))  // descendant, not held
	assert.False(t, rs.HasRole([]string{"viewer"}, "ghost"))
}

func TestResolveListsEffectivePermissions(t *testing.T) {
	rs := mkSet(t)
	ps := rs.Resolve("editor")
	assert.True(t, ps.Allows("documents:write"))
	assert.True(t, ps.Allows("comments:read"))
	assert.ElementsMatch(t, []string{"documents:*", "comments:read", "documents:read"}, ps.List())
}

func TestNewRoleSetErrors(t *testing.T) {
	_, err := rbac.NewRoleSet(rbac.WithRoles(rbac.Role("a", "x"), rbac.Role("a", "y")))
	assert.ErrorIs(t, err, rbac.ErrDuplicateRole)

	_, err = rbac.NewRoleSet(
		rbac.WithRoles(rbac.Role("a", "x")),
		rbac.WithRoleInheritance(rbac.RoleInherits("a", "missing")),
	)
	assert.ErrorIs(t, err, rbac.ErrUnknownRole)

	_, err = rbac.NewRoleSet(
		rbac.WithRoles(rbac.Role("a", "x"), rbac.Role("b", "y")),
		rbac.WithRoleInheritance(rbac.RoleInherits("a", "b"), rbac.RoleInherits("b", "a")),
	)
	assert.ErrorIs(t, err, rbac.ErrCycle)
}
