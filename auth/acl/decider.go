package acl

import (
	"context"

	"github.com/dmitrymomot/forge/auth/access"
)

// Decider adapts the entry Store to the access seam. It fetches the subject's
// entries on (Resource.Type, Resource.ID) — type-wide entries included — and
// applies deny-wins: any matching Deny entry → Deny, else any matching Allow
// entry → Allow, else Abstain (rbac or another layer may still grant). The
// tenant is Subject.Tenant, so entries never cross tenants. A Store error
// abstains and returns the error so combinators fail the chain closed.
func Decider(store Store) access.Decider {
	return access.DeciderFunc(func(ctx context.Context, s access.Subject, a access.Action, r access.Resource) (access.Decision, error) {
		entries, err := store.EntriesFor(ctx, s.Tenant, s.ID, r.Type, r.ID)
		if err != nil {
			return access.Decision{Effect: access.Abstain, Decider: "acl"}, err
		}
		allowed := false
		for _, e := range entries {
			if !e.matches(r.Type, r.ID, a) {
				continue
			}
			switch e.Effect {
			case access.Deny:
				return access.Decision{Effect: access.Deny, Decider: "acl", Reason: "explicit deny"}, nil
			case access.Allow:
				allowed = true
			}
		}
		if allowed {
			return access.Decision{Effect: access.Allow, Decider: "acl", Reason: "explicit grant"}, nil
		}
		return access.Decision{Effect: access.Abstain, Decider: "acl", Reason: "no matching entry"}, nil
	})
}
