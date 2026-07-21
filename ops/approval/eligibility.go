package approval

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
)

// eligible asks the decision seam whether a may act on r.
//
// The action is "<kind>:<verb>" — "payout.release:decide" for a vote,
// "payout.release:cancel" for a third-party withdrawal. Both votes share
// one verb: being a checker is the privilege, and which way a checker votes
// is not a separate grant.
//
// It fails closed. access.Authorize already collapses Abstain and decider
// errors into Deny, so anything short of an explicit Allow refuses the
// action. A decider that cannot reach its data must never let a payout
// through.
func (m *Manager) eligible(ctx context.Context, r Request, a Actor, verb string) error {
	if m.cfg.decider == nil {
		return nil
	}
	res := access.Resource{
		Type:   "approval",
		ID:     r.ID.String(),
		Tenant: r.Tenant,
		Attrs: map[string]any{
			"kind":      r.Kind,
			"requester": r.Requester,
			// The raw payload, not a decoded map: value-aware rules decode
			// it themselves, and rules that ignore it pay nothing.
			"payload": r.Payload,
		},
	}
	dec, err := access.Authorize(ctx, m.cfg.decider, a.Subject, access.Action(r.Kind+":"+verb), res)
	if err != nil {
		// Never surface the decider's raw error to the caller: it may carry
		// internal detail, and the caller sees a generic refusal either way.
		return fmt.Errorf("%w: %s", ErrNotEligible, "eligibility could not be established")
	}
	if dec.Effect != access.Allow {
		return ErrNotEligible
	}
	return nil
}
