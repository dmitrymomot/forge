package rbac_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/rbac"
)

func Example() {
	rs, err := rbac.NewRoleSet(
		rbac.WithRoles(
			rbac.Role("viewer", "documents:read"),
			rbac.Role("editor", "documents:*"),
			rbac.Role("admin", "*"),
		),
		rbac.WithRoleInheritance(
			rbac.RoleInherits("editor", "viewer"),
		),
	)
	if err != nil {
		panic(err)
	}

	d := rbac.Decider(rs, rbac.FromSubject())
	dec, _ := access.Authorize(
		context.Background(), d,
		access.Subject{ID: "u1", Roles: []string{"editor"}},
		"documents:write", access.Resource{},
	)
	fmt.Println(dec.Effect)
	// Output: allow
}
