package rbac

import (
	"fmt"
	"strings"
)

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

// RoleSet is an immutable, concurrent-safe set of role definitions with
// inheritance resolved. Effective permission sets and ancestor closures are
// pre-expanded per role so Can/HasRole are map lookups.
type RoleSet struct {
	effective map[string]*permSet            // role -> effective (own + inherited) grants
	ancestors map[string]map[string]struct{} // role -> {self + all inherited role names}
	grants    map[string][]string            // role -> effective grant patterns (for Resolve.List)
}

// NewRoleSet validates the definitions and pre-expands inheritance. Errors:
// ErrDuplicateRole, ErrUnknownRole, ErrCycle.
func NewRoleSet(opts ...RoleSetOption) (*RoleSet, error) {
	cfg := roleSetConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	own := make(map[string][]string, len(cfg.roles)) // role -> own grants
	for _, d := range cfg.roles {
		if _, dup := own[d.name]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateRole, d.name)
		}
		own[d.name] = d.grants
	}

	// An inheritance edge's child defines a role even if it has no own grants
	// (a "pure aggregator", e.g. manager = editor + auditor). Parents, however,
	// must be roles that exist (in WithRoles or as another edge's child).
	parents := make(map[string][]string, len(cfg.edges)) // role -> direct parents
	for _, e := range cfg.edges {
		if _, ok := own[e.child]; !ok {
			own[e.child] = nil // pure aggregator: known, but grants nothing itself
		}
		parents[e.child] = append(parents[e.child], e.parents...)
	}
	for _, e := range cfg.edges {
		for _, p := range e.parents {
			if _, ok := own[p]; !ok {
				return nil, fmt.Errorf("%w: %q", ErrUnknownRole, p)
			}
		}
	}

	rs := &RoleSet{
		effective: make(map[string]*permSet, len(own)),
		ancestors: make(map[string]map[string]struct{}, len(own)),
		grants:    make(map[string][]string, len(own)),
	}

	// Depth-first closure per role with cycle detection.
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(own))
	closure := make(map[string]map[string]struct{}, len(own)) // role -> ancestor names

	var walk func(role string) (map[string]struct{}, error)
	walk = func(role string) (map[string]struct{}, error) {
		if state[role] == done {
			return closure[role], nil
		}
		if state[role] == visiting {
			return nil, fmt.Errorf("%w: at %q", ErrCycle, role)
		}
		state[role] = visiting
		acc := map[string]struct{}{role: {}}
		for _, p := range parents[role] {
			pc, err := walk(p)
			if err != nil {
				return nil, err
			}
			for name := range pc {
				acc[name] = struct{}{}
			}
		}
		state[role] = done
		closure[role] = acc
		return acc, nil
	}

	for role := range own {
		acc, err := walk(role)
		if err != nil {
			return nil, err
		}
		var eff []string
		for name := range acc {
			eff = append(eff, own[name]...)
		}
		rs.ancestors[role] = acc
		rs.effective[role] = newPermSet(eff)
		rs.grants[role] = eff
	}
	return rs, nil
}

// Can reports whether any of roleNames grants action (through inheritance and
// wildcards). Zero-alloc; short-circuits on the first grant. Unknown role
// names contribute nothing.
func (rs *RoleSet) Can(roleNames []string, action string) bool {
	for _, r := range roleNames {
		if p, ok := rs.effective[r]; ok && p.allows(action) {
			return true
		}
	}
	return false
}

// HasRole reports whether required is in the inheritance closure of roleNames
// (holding editor, which inherits viewer, satisfies HasRole(…, "viewer")).
// Zero-alloc. Unknown role names contribute nothing.
func (rs *RoleSet) HasRole(roleNames []string, required string) bool {
	for _, r := range roleNames {
		if anc, ok := rs.ancestors[r]; ok {
			if _, has := anc[required]; has {
				return true
			}
		}
	}
	return false
}

// PermissionSet is the effective permission set of some roles — for listing,
// debugging, and admin UIs. Not the hot path.
type PermissionSet struct {
	set      *permSet
	patterns []string
}

// Resolve returns the union of the effective permissions of roleNames.
func (rs *RoleSet) Resolve(roleNames ...string) PermissionSet {
	var pats []string
	seen := map[string]struct{}{}
	for _, r := range roleNames {
		for _, g := range rs.grants[r] {
			if _, dup := seen[g]; dup {
				continue
			}
			seen[g] = struct{}{}
			pats = append(pats, g)
		}
	}
	return PermissionSet{set: newPermSet(pats), patterns: pats}
}

// Allows reports whether action is granted by this set.
func (ps PermissionSet) Allows(action string) bool {
	if ps.set == nil {
		return false
	}
	return ps.set.allows(action)
}

// List returns the effective grant patterns (deduped, unordered).
func (ps PermissionSet) List() []string { return ps.patterns }

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
