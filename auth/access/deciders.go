package access

import (
	"context"
	"slices"
)

// ScopeDecider allows when the subject's Scopes contain the action string
// verbatim, else abstains. Exact match only — wildcard/prefix grants are rbac's
// job. This is the day-one usable path: put permissions in token scopes.
// Reasons are constant so the Decide path stays zero-alloc.
func ScopeDecider() Decider {
	return DeciderFunc(func(_ context.Context, s Subject, a Action, _ Resource) (Decision, error) {
		if slices.Contains(s.Scopes, string(a)) {
			return Decision{Effect: Allow, Decider: "scope", Reason: "action in scopes"}, nil
		}
		return Decision{Effect: Abstain, Decider: "scope", Reason: "action not in scopes"}, nil
	})
}

// TenantMatch denies when Subject.Tenant and Resource.Tenant are both set and
// differ; abstains otherwise. Placed first in a chain it makes cross-tenant
// access impossible by construction. Single-tenant apps leave both empty and
// it always abstains.
func TenantMatch() Decider {
	return DeciderFunc(func(_ context.Context, s Subject, _ Action, r Resource) (Decision, error) {
		if s.Tenant != "" && r.Tenant != "" && s.Tenant != r.Tenant {
			return Decision{Effect: Deny, Decider: "tenant", Reason: "cross-tenant access"}, nil
		}
		return Decision{Effect: Abstain, Decider: "tenant", Reason: "same or unscoped tenant"}, nil
	})
}

// AllowAll always allows — a terminal for tests and explicit open policies.
func AllowAll() Decider {
	return DeciderFunc(func(_ context.Context, _ Subject, _ Action, _ Resource) (Decision, error) {
		return Decision{Effect: Allow, Decider: "allow-all", Reason: "allow-all"}, nil
	})
}

// DenyAll always denies with the given reason — a terminal for tests and
// explicit closed policies.
func DenyAll(reason string) Decider {
	return DeciderFunc(func(_ context.Context, _ Subject, _ Action, _ Resource) (Decision, error) {
		return Decision{Effect: Deny, Decider: "deny-all", Reason: reason}, nil
	})
}
