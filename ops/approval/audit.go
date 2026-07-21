package approval

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

// audit records a completed transition.
//
// It runs AFTER the store write, never before: auditing first would record
// transitions that a lost CAS race then discarded. The cost of that
// ordering is that a failed trail write cannot undo a durable transition,
// so the caller gets the updated Request AND an ErrAuditFailed-wrapped
// error. Match it with errors.Is and alert — in a dual-control package a
// silent gap in the trail is the failure mode that matters.
func (m *Manager) audit(ctx context.Context, r Request, action, actor, outcome, reason string) error {
	if m.cfg.auditor == nil {
		return nil
	}
	meta := map[string]string{
		"kind":   r.Kind,
		"status": r.Status.String(),
	}
	if reason != "" {
		meta["reason"] = reason
	}
	if action == actionApprove || action == actionReject {
		if pol, ok := m.policyFor(r.Kind); ok {
			meta["quorum"] = strconv.Itoa(pol.Quorum)
			meta["approvals"] = strconv.Itoa(r.Approvals())
		}
		if n := len(r.Decisions); n > 0 {
			meta["vote"] = r.Decisions[n-1].Vote.String()
		}
	}
	_, err := m.cfg.auditor.Record(ctx, auditlog.Event{
		Actor:    actor,
		Action:   action,
		Resource: "approval:" + r.ID.String(),
		Outcome:  auditlog.Outcome(outcome),
		Tenant:   r.Tenant,
		Meta:     meta,
	})
	if err != nil {
		// action is folded in so that two ErrAuditFailed wrappings joined
		// together (e.g. Execute's claim write and its Complete/Fail write
		// both failing) remain distinguishable in the error text — see
		// TestExecuteJoinsDistinctAuditFailures.
		return fmt.Errorf("%w for %s: %w", ErrAuditFailed, action, err)
	}
	return nil
}

// auditDenied records a refused attempt. An ineligible actor trying to push
// a request through is the most security-relevant event this package sees,
// so it is never invisible — even though no state changed.
//
// tenant is the request's own tenant (empty when unscoped), not the
// caller's — that's what lets a denied event be attributed to the right
// tenant in the trail.
func (m *Manager) auditDenied(ctx context.Context, reqID id.UUID, action, actor, tenant string, cause error) error {
	if m.cfg.auditor == nil {
		return nil
	}
	_, err := m.cfg.auditor.Record(ctx, auditlog.Event{
		Actor:    actor,
		Action:   action,
		Resource: "approval:" + reqID.String(),
		Outcome:  auditlog.OutcomeDenied,
		Tenant:   tenant,
		Meta:     map[string]string{"cause": cause.Error()},
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuditFailed, err)
	}
	return nil
}
