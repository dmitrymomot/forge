package access

import "context"

type explainKey struct{}

// ExplainContext marks ctx so combinators accumulate the full per-layer trace
// into Decision.Trace. Off by default so the hot path allocates no trace slice.
// RequirePermission/Model expose WithExplain() to enable it for a debug request
// or an "explain" endpoint.
func ExplainContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, explainKey{}, true)
}

func explaining(ctx context.Context) bool {
	v, _ := ctx.Value(explainKey{}).(bool)
	return v
}

// FirstDecisive returns the first decider's Allow or Deny; Abstain falls
// through. All-abstain returns Abstain. Passing [TenantMatch(), acl, abac,
// rbac] reproduces the documented precedence exactly. A decider error stops
// evaluation and returns a fail-closed Deny plus the wrapped error.
func FirstDecisive(deciders ...Decider) Decider {
	return DeciderFunc(func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
		trace := explaining(ctx)
		var acc []Decision
		for _, d := range deciders {
			dec, err := d.Decide(ctx, s, a, r)
			if trace {
				acc = append(acc, dec)
			}
			if err != nil {
				return Decision{Effect: Deny, Decider: firstNonEmpty(dec.Decider, "access"), Reason: err.Error(), Trace: acc}, err
			}
			if dec.Effect != Abstain {
				dec.Trace = acc
				return dec, nil
			}
		}
		return Decision{Effect: Abstain, Trace: acc}, nil
	})
}

// DenyOverrides evaluates deciders until a Deny vetoes: any Deny wins
// regardless of order, so evaluation continues past an Allow to catch a later
// Deny, and stops at the first Deny (the outcome is sealed). With no Deny, the
// first Allow wins; with neither, Abstain. A decider error stops evaluation and
// returns a fail-closed Deny plus the wrapped error. Under WithExplain the trace
// therefore ends at the vetoing Deny.
func DenyOverrides(deciders ...Decider) Decider {
	return DeciderFunc(func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
		trace := explaining(ctx)
		var acc []Decision
		var firstAllow Decision
		haveAllow := false
		for _, d := range deciders {
			dec, err := d.Decide(ctx, s, a, r)
			if trace {
				acc = append(acc, dec)
			}
			if err != nil {
				return Decision{Effect: Deny, Decider: firstNonEmpty(dec.Decider, "access"), Reason: err.Error(), Trace: acc}, err
			}
			switch dec.Effect {
			case Deny:
				dec.Trace = acc
				return dec, nil
			case Allow:
				if !haveAllow {
					firstAllow = dec
					haveAllow = true
				}
			}
		}
		if haveAllow {
			firstAllow.Trace = acc
			return firstAllow, nil
		}
		return Decision{Effect: Abstain, Trace: acc}, nil
	})
}
