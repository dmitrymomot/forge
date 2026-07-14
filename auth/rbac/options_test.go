package rbac_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/auth/rbac"
)

func TestRoleAndInheritConstructors(t *testing.T) {
	// Constructors are opaque values consumed by the options; we assert they
	// compile and thread through NewRoleSet without error (behavior verified
	// in Task 7). Here we only pin the option surface.
	_, err := rbac.NewRoleSet(
		rbac.WithRoles(
			rbac.Role("viewer", "documents:read"),
			rbac.Role("admin", "*"),
		),
		rbac.WithRoleInheritance(
			rbac.RoleInherits("admin", "viewer"),
		),
	)
	assert.NoError(t, err)
}
