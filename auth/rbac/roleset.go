package rbac

import "strings"

// RoleDef is a role and its own granted permission patterns, built by Role.
type RoleDef struct {
	name   string
	grants []string
}

// Role declares a role named name granting the given permission patterns
// (exact "documents:read", segment wildcard "documents:*", or super "*").
func Role(name string, grants ...string) RoleDef {
	return RoleDef{name: name, grants: grants}
}

// InheritEdge is one inheritance edge (child inherits from parents), built by
// RoleInherits. Multi-parent is supported.
type InheritEdge struct {
	child   string
	parents []string
}

// RoleInherits declares that child inherits the permissions of every parent.
func RoleInherits(child string, parents ...string) InheritEdge {
	return InheritEdge{child: child, parents: parents}
}

type roleSetConfig struct {
	roles []RoleDef
	edges []InheritEdge
}

// RoleSetOption configures NewRoleSet.
type RoleSetOption func(*roleSetConfig)

// WithRoles adds role definitions. Accumulates across calls.
func WithRoles(defs ...RoleDef) RoleSetOption {
	return func(c *roleSetConfig) { c.roles = append(c.roles, defs...) }
}

// WithRoleInheritance adds inheritance edges. Accumulates across calls.
func WithRoleInheritance(edges ...InheritEdge) RoleSetOption {
	return func(c *roleSetConfig) { c.edges = append(c.edges, edges...) }
}

// NewRoleSet is completed in Task 7; this stub lets the option surface compile.
func NewRoleSet(opts ...RoleSetOption) (*RoleSet, error) {
	cfg := roleSetConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return &RoleSet{}, nil
}

// RoleSet is fleshed out in Task 7.
type RoleSet struct{}

// permSet is a role's effective, pre-expanded permission set. Lookups are O(1)
// and allocation-free.
type permSet struct {
	exact    map[string]struct{} // "documents:read"
	wildcard map[string]struct{} // noun before ":*", stored bare: "documents"
	super    bool                // "*" grants everything
}

// newPermSet classifies grant patterns into the three match forms.
func newPermSet(grants []string) *permSet {
	p := &permSet{exact: map[string]struct{}{}, wildcard: map[string]struct{}{}}
	for _, g := range grants {
		switch {
		case g == "*":
			p.super = true
		case strings.HasSuffix(g, ":*"):
			p.wildcard[g[:len(g)-2]] = struct{}{}
		default:
			p.exact[g] = struct{}{}
		}
	}
	return p
}

// allows reports whether action is granted. Zero-alloc: Cut returns substrings
// that share action's backing array, so the noun lookup never allocates.
func (p *permSet) allows(action string) bool {
	if p.super {
		return true
	}
	if _, ok := p.exact[action]; ok {
		return true
	}
	noun, _, found := strings.Cut(action, ":")
	if !found {
		return false
	}
	_, ok := p.wildcard[noun]
	return ok
}
