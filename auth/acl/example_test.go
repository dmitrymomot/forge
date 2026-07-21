package acl_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/acl"
	"github.com/dmitrymomot/forge/auth/rbac"
)

func Example() {
	ctx := context.Background()
	store := acl.NewMemoryStore()
	admin := acl.NewManager(store)
	_ = admin.Grant(ctx, "mgr-7", "agent", "42", "agents:read")
	_ = admin.Deny(ctx, "mgr-7", "agent", "13", "agents:read")

	d := acl.Decider(store)
	for _, id := range []string{"42", "13", "99"} {
		dec, _ := d.Decide(ctx, access.Subject{ID: "mgr-7"}, "agents:read", access.Resource{Type: "agent", ID: id})
		fmt.Println(id, dec.Effect, "—", dec.Reason)
	}
	// Output:
	// 42 allow — explicit grant
	// 13 deny — explicit deny
	// 99 abstain — no matching entry
}

// An ACL deny placed before rbac vetoes what the role would allow.
func Example_vetoRoleGrant() {
	ctx := context.Background()
	rs, _ := rbac.NewRoleSet(rbac.WithRoles(rbac.Role("viewer", "documents:read")))

	store := acl.NewMemoryStore()
	_ = acl.NewManager(store).Deny(ctx, "u1", "document", "13", "documents:read")

	decider := access.FirstDecisive(
		access.TenantMatch(),
		acl.Decider(store),
		rbac.Decider(rs, rbac.FromSubject()),
	)

	sub := access.Subject{ID: "u1", Roles: []string{"viewer"}}
	for _, id := range []string{"7", "13"} {
		dec, _ := access.Authorize(ctx, decider, sub, "documents:read", access.Resource{Type: "document", ID: id})
		fmt.Println(id, dec.Effect, "by", dec.Decider)
	}
	// Output:
	// 7 allow by rbac
	// 13 deny by acl
}
