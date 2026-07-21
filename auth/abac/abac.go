package abac

import (
	"context"
	"fmt"
	"strings"

	"github.com/dmitrymomot/forge/auth/access"
)

// Predicate is a registered Go function answering an attribute/relationship
// question about a subject acting on a resource: ownership, tree membership,
// assignment. The relationship data backing the answer stays in consumer code
// feeding the predicate — abac never fetches anything. A non-nil error fails
// the decision closed through the access seam. Implementations must be safe
// for concurrent use.
type Predicate func(ctx context.Context, s access.Subject, r access.Resource) (bool, error)

// Rule binds a Predicate to the action pattern and resource type it governs.
// Build rules with Allow and Deny; the zero Rule is rejected by New.
type Rule struct {
	pred     Predicate
	name     string
	action   string
	resource string
	effect   access.Effect
}

// Allow builds a rule granting action on resourceType when pred is true.
// action is exact ("documents:read"), a noun wildcard ("documents:*"), or the
// super pattern "*"; resourceType is an exact Resource.Type or "*" for any.
// name labels the rule in decision reasons and errors.
func Allow(name, action, resourceType string, pred Predicate) Rule {
	return Rule{name: name, action: action, resource: resourceType, pred: pred, effect: access.Allow}
}

// Deny builds a veto rule: when action and resourceType match and pred is
// true, the decision is Deny. Deny rules are evaluated before allow rules, so
// a satisfied veto wins regardless of registration order. Patterns are as in
// Allow.
func Deny(name, action, resourceType string, pred Predicate) Rule {
	return Rule{name: name, action: action, resource: resourceType, pred: pred, effect: access.Deny}
}

// action pattern kinds, classified once in New so Decide is comparisons only.
const (
	matchExact uint8 = iota
	matchNoun        // "documents:*" — noun stored bare
	matchAny         // "*"
)

// compiledRule is a Rule with its pattern classified and its decision reason
// prebuilt, so the Decide path is allocation-free.
type compiledRule struct {
	pred        Predicate
	name        string
	action      string // exact action, or bare noun for matchNoun
	resource    string // exact type; ignored when anyResource
	reason      string
	actionKind  uint8
	anyResource bool
}

// matches reports whether the rule governs (action, resourceType). Zero-alloc:
// strings.Cut returns substrings sharing action's backing array.
func (cr *compiledRule) matches(action, resourceType string) bool {
	switch cr.actionKind {
	case matchAny:
	case matchNoun:
		noun, _, found := strings.Cut(action, ":")
		if !found || noun != cr.action {
			return false
		}
	default:
		if action != cr.action {
			return false
		}
	}
	return cr.anyResource || cr.resource == resourceType
}

// Policy is an immutable, concurrent-safe set of rules; it implements
// access.Decider and drops into a FirstDecisive/DenyOverrides chain alongside
// rbac and acl.
type Policy struct {
	denies []compiledRule
	allows []compiledRule
}

// New validates the rules added via WithRules and compiles them into a Policy.
// Errors: ErrUnnamedRule, ErrDuplicateRule, ErrNilPredicate, ErrEmptyAction,
// ErrEmptyResource.
func New(opts ...Option) (*Policy, error) {
	cfg := config{}
	for _, o := range opts {
		o(&cfg)
	}
	p := &Policy{}
	seen := make(map[string]struct{}, len(cfg.rules))
	for _, r := range cfg.rules {
		if r.name == "" {
			return nil, ErrUnnamedRule
		}
		if _, dup := seen[r.name]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateRule, r.name)
		}
		seen[r.name] = struct{}{}
		if r.pred == nil {
			return nil, fmt.Errorf("%w: rule %q", ErrNilPredicate, r.name)
		}
		if r.action == "" {
			return nil, fmt.Errorf("%w: rule %q", ErrEmptyAction, r.name)
		}
		if r.resource == "" {
			return nil, fmt.Errorf("%w: rule %q", ErrEmptyResource, r.name)
		}

		cr := compiledRule{pred: r.pred, name: r.name, action: r.action, resource: r.resource}
		switch {
		case r.action == "*":
			cr.actionKind = matchAny
		case strings.HasSuffix(r.action, ":*"):
			cr.actionKind = matchNoun
			cr.action = r.action[:len(r.action)-2]
		}
		if r.resource == "*" {
			cr.anyResource = true
		}
		if r.effect == access.Deny {
			cr.reason = fmt.Sprintf("denied by rule %q", r.name)
			p.denies = append(p.denies, cr)
		} else {
			cr.reason = fmt.Sprintf("allowed by rule %q", r.name)
			p.allows = append(p.allows, cr)
		}
	}
	return p, nil
}

const deciderName = "abac"

// Decide implements access.Decider. Deny rules are evaluated first — the first
// satisfied veto returns Deny — then allow rules — the first satisfied grant
// returns Allow. With no rule matched or no predicate satisfied it abstains so
// lower-precedence layers (rbac, scopes) can decide. A predicate error stops
// evaluation and is returned wrapped with the rule name; the seam fails it
// closed.
func (p *Policy) Decide(ctx context.Context, s access.Subject, a access.Action, r access.Resource) (access.Decision, error) {
	action := string(a)
	matched := false
	for i := range p.denies {
		cr := &p.denies[i]
		if !cr.matches(action, r.Type) {
			continue
		}
		matched = true
		ok, err := cr.pred(ctx, s, r)
		if err != nil {
			return access.Decision{Effect: access.Abstain, Decider: deciderName}, fmt.Errorf("abac: rule %q: %w", cr.name, err)
		}
		if ok {
			return access.Decision{Effect: access.Deny, Decider: deciderName, Reason: cr.reason}, nil
		}
	}
	for i := range p.allows {
		cr := &p.allows[i]
		if !cr.matches(action, r.Type) {
			continue
		}
		matched = true
		ok, err := cr.pred(ctx, s, r)
		if err != nil {
			return access.Decision{Effect: access.Abstain, Decider: deciderName}, fmt.Errorf("abac: rule %q: %w", cr.name, err)
		}
		if ok {
			return access.Decision{Effect: access.Allow, Decider: deciderName, Reason: cr.reason}, nil
		}
	}
	reason := "no rule matched"
	if matched {
		reason = "no predicate satisfied"
	}
	return access.Decision{Effect: access.Abstain, Decider: deciderName, Reason: reason}, nil
}
