package approval

import (
	"context"
	"errors"

	"github.com/dmitrymomot/forge/core/id"
)

// Approve records an approving decision from a. When it is the quorum-th
// distinct approval the request becomes Approved.
//
// It refuses the request's own requester (ErrSelfApproval) at every quorum
// — including quorum 1, which is still dual control — and refuses a second
// decision from an approver who has already voted (ErrAlreadyVoted).
func (m *Manager) Approve(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	return m.vote(ctx, reqID, a, VoteApprove)
}

// Reject records a rejecting decision from a. A single rejection is
// terminal: there is no reject quorum, because one checker seeing a problem
// is reason enough to stop.
func (m *Manager) Reject(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	return m.vote(ctx, reqID, a, VoteReject)
}

func (m *Manager) vote(ctx context.Context, reqID id.UUID, a Actor, v Vote) (Request, error) {
	if a.Subject.ID == "" {
		return Request{}, ErrActorRequired
	}
	action := actionApprove
	if v == VoteReject {
		action = actionReject
	}

	var denied bool
	var tenant string
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		tenant = r.Tenant
		if r.Status != Pending {
			return statusErr(r.Status)
		}
		if r.Requester == a.Subject.ID {
			return ErrSelfApproval
		}
		for i := range r.Decisions {
			if r.Decisions[i].Approver == a.Subject.ID {
				return ErrAlreadyVoted
			}
		}
		// The decider runs last: it may hit a database or an org chart, so
		// it only pays off once the vote is otherwise legal.
		if err := m.eligible(ctx, *r, a, verbDecide); err != nil {
			denied = true
			return err
		}

		now := m.now()
		r.Decisions = append(r.Decisions, Decision{
			At:       now,
			Approver: a.Subject.ID,
			Reason:   a.Reason,
			Vote:     v,
		})
		pol, ok := m.policyFor(r.Kind)
		if !ok {
			return ErrUnknownKind
		}
		switch {
		case v == VoteReject:
			r.Status = Rejected
			r.DecidedAt = now
		case r.Approvals() >= pol.Quorum:
			r.Status = Approved
			r.DecidedAt = now
		}
		return nil
	})
	if err != nil {
		if denied {
			// An ineligible actor trying to push a request through is the
			// most security-relevant event this package sees. Record it, and
			// join the audit error with the caller's own: errors.Is must still
			// hold for both the original sentinel and ErrAuditFailed.
			if aerr := m.auditDenied(ctx, reqID, action, a.Subject.ID, tenant, err); aerr != nil {
				return Request{}, errors.Join(err, aerr)
			}
		}
		return Request{}, err
	}
	return r, m.audit(ctx, r, action, a.Subject.ID, outcomeSuccess, a.Reason)
}

// Cancel withdraws a request before it executes. It is legal from Pending
// and Approved but not from Executing — an in-flight action is not
// cancellable; the executor reports the outcome with Complete or Fail.
//
// The requester may always cancel their own request. Any other actor is
// gated on "<kind>:cancel" when a decider is configured: withdrawing
// someone else's request is a different privilege from judging it, and a
// policy that grants one need not grant the other.
func (m *Manager) Cancel(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	if a.Subject.ID == "" {
		return Request{}, ErrActorRequired
	}

	var denied bool
	var tenant string
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		tenant = r.Tenant
		if r.Status != Pending && r.Status != Approved {
			if r.Status == Expired {
				return ErrExpired
			}
			return ErrNotCancellable
		}
		if r.Requester != a.Subject.ID {
			if err := m.eligible(ctx, *r, a, verbCancel); err != nil {
				denied = true
				return err
			}
		}
		r.Status = Cancelled
		r.DecidedAt = m.now()
		return nil
	})
	if err != nil {
		if denied {
			// Join, don't drop: the actor's denial and the audit failure are
			// distinct facts and both must survive errors.Is.
			if aerr := m.auditDenied(ctx, reqID, actionCancel, a.Subject.ID, tenant, err); aerr != nil {
				return Request{}, errors.Join(err, aerr)
			}
		}
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionCancel, a.Subject.ID, outcomeSuccess, a.Reason)
}
