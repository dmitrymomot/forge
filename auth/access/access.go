package access

import (
	"context"

	"github.com/dmitrymomot/forge/auth/guard"
)

// Subject is the actor a decision is made about. It is its own type rather
// than a reuse of guard.Identity so the seam works off the HTTP hot path too
// (a background job has no Identity). Attrs is the abac attribute bag.
type Subject struct {
	Attrs  map[string]any // abac reads these; nil-safe
	ID     string         // principal id
	Tenant string         // optional tenant scope
	Scopes []string       // token scopes; ScopeDecider reads these
}

// Action is a verb-on-noun permission string, e.g. "documents:read".
type Action string

// Resource is the thing acted upon. Attrs are caller-supplied — access never
// fetches them. ID == "" means a type-level / collection check.
type Resource struct {
	Attrs  map[string]any
	Type   string
	ID     string
	Tenant string // enables the built-in cross-tenant veto (TenantMatch)
}

// Effect is a three-valued authorization outcome. The zero value Abstain
// ("no opinion") lets a layer decline so a lower-precedence layer can decide.
type Effect uint8

const (
	Abstain Effect = iota // no opinion — fall through
	Allow
	Deny
)

// String renders the effect for logs and traces.
func (e Effect) String() string {
	switch e {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	default:
		return "abstain"
	}
}

// Because builds a Decision carrying this effect and a human reason, keeping
// custom deciders terse: `return access.Allow.Because("admin role"), nil`.
func (e Effect) Because(reason string) Decision {
	return Decision{Effect: e, Reason: reason}
}

// Decision is the explanation record for one authorization outcome. Decider
// names the layer that spoke; Reason says why. Trace holds every consulted
// layer's decision and is populated only when the context enables it
// (WithExplain); it is nil on the hot path.
//
// Security: Reason (and Trace reasons) may carry internal detail — a decider's
// raw error text lands in Reason on the fail-closed path. It is safe for
// server-side auditlog, but a custom WithResponder/WithLoadError must not echo
// it verbatim to clients; the built-in responders send generic sentinels.
type Decision struct {
	Decider string
	Reason  string
	Trace   []Decision
	Effect  Effect
}

// Decider answers the authorization question. A non-nil error is fail-closed:
// combinators and Authorize turn it into Deny. Implementations must be safe
// for concurrent use.
type Decider interface {
	Decide(ctx context.Context, s Subject, a Action, r Resource) (Decision, error)
}

// DeciderFunc adapts a function to Decider — the package's test fake and the
// shape rbac/acl/abac deciders and ad-hoc consumer predicates take.
type DeciderFunc func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error)

// Decide implements Decider.
func (f DeciderFunc) Decide(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
	return f(ctx, s, a, r)
}

// Named wraps d so its decisions carry name in Decision.Decider when the
// wrapped decider left it empty. Built-in deciders name themselves; Named
// labels ad-hoc closures for the explanation record.
func Named(name string, d Decider) Decider {
	return DeciderFunc(func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
		dec, err := d.Decide(ctx, s, a, r)
		if dec.Decider == "" {
			dec.Decider = name
		}
		return dec, err
	})
}

// SubjectFromIdentity adapts a guard.Identity into a Subject. It is zero-alloc:
// it copies ID, Tenant, and the Scopes slice header (shared, read-only) and
// leaves Attrs nil. guard.Identity.Meta is NOT promoted into Attrs — that
// would allocate a map on every request and the common scope/tenant checks
// never read it. Consumers needing identity metadata in predicates populate
// Attrs themselves via a WithSubject resolver.
func SubjectFromIdentity(id guard.Identity) Subject {
	return Subject{ID: id.Subject, Tenant: id.Tenant, Scopes: id.Scopes}
}

// Authorize runs d.Decide and closes it fail-closed: Abstain becomes a Deny
// ("no layer granted"); a decider error becomes a Deny with the error returned
// separately so in-handler callers may answer 503 instead of 403. The returned
// Decision is always decisive (Allow or Deny).
func Authorize(ctx context.Context, d Decider, s Subject, a Action, r Resource) (Decision, error) {
	dec, err := d.Decide(ctx, s, a, r)
	if err != nil {
		return Decision{Effect: Deny, Decider: firstNonEmpty(dec.Decider, "access"), Reason: err.Error(), Trace: dec.Trace}, err
	}
	if dec.Effect == Abstain {
		return Decision{Effect: Deny, Decider: "access", Reason: "no layer granted", Trace: dec.Trace}, nil
	}
	return dec, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
