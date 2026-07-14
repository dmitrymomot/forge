package rbac

import (
	"context"

	"github.com/dmitrymomot/forge/auth/access"
)

// RoleSource yields a subject's role names for a decision.
type RoleSource interface {
	Roles(ctx context.Context, s access.Subject) ([]string, error)
}

type roleSourceFunc func(ctx context.Context, s access.Subject) ([]string, error)

func (f roleSourceFunc) Roles(ctx context.Context, s access.Subject) ([]string, error) {
	return f(ctx, s)
}

// FromSubject reads roles straight off the Subject (populated from
// guard.Identity.Roles). Zero-I/O fast path.
func FromSubject() RoleSource {
	return roleSourceFunc(func(_ context.Context, s access.Subject) ([]string, error) {
		return s.Roles, nil
	})
}

// FromStore reads roles from the assignment Store for (Subject.Tenant,
// Subject.ID) — the runtime-assigned-roles path.
func FromStore(store Store) RoleSource {
	return roleSourceFunc(func(ctx context.Context, s access.Subject) ([]string, error) {
		return store.RolesFor(ctx, s.Tenant, s.ID)
	})
}

// Decider adapts the engine to the access seam: Allow when the subject's roles
// grant the action, else Abstain (never Deny — a missing grant must not veto
// acl/abac). A RoleSource error is returned so the chain fails closed.
func Decider(rs *RoleSet, src RoleSource) access.Decider {
	return access.DeciderFunc(func(ctx context.Context, s access.Subject, a access.Action, _ access.Resource) (access.Decision, error) {
		roles, err := src.Roles(ctx, s)
		if err != nil {
			return access.Decision{Effect: access.Abstain, Decider: "rbac"}, err
		}
		if rs.Can(roles, string(a)) {
			return access.Decision{Effect: access.Allow, Decider: "rbac", Reason: "role grants permission"}, nil
		}
		return access.Decision{Effect: access.Abstain, Decider: "rbac", Reason: "no role grants permission"}, nil
	})
}
