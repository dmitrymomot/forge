package access_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
)

// Example wires the built-in deciders into the documented precedence and
// resolves a decision fail-closed with Authorize.
func Example() {
	decider := access.FirstDecisive(
		access.TenantMatch(),  // cross-tenant → hard deny
		access.ScopeDecider(), // action in token scopes → allow
	)

	sub := access.Subject{ID: "u1", Tenant: "t1", Scopes: []string{"documents:read"}}
	res := access.Resource{Type: "document", ID: "42", Tenant: "t1"}

	dec, _ := access.Authorize(context.Background(), decider, sub, "documents:read", res)
	fmt.Printf("%s by %s\n", dec.Effect, dec.Decider)

	// Cross-tenant is denied by construction.
	other := access.Resource{Type: "document", ID: "99", Tenant: "t2"}
	dec, _ = access.Authorize(context.Background(), decider, sub, "documents:read", other)
	fmt.Printf("%s by %s\n", dec.Effect, dec.Decider)

	// Output:
	// allow by scope
	// deny by tenant
}
