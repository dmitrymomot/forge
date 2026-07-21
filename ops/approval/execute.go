package approval

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Claim takes exclusive ownership of an approved request so its action runs
// once. It is the transition that makes double execution impossible: two
// operators clicking the same button, or two webhook deliveries racing,
// produce exactly one winner and one ErrAlreadyClaimed.
//
// It is legal only from Approved, or from Executing when the current claim
// has gone stale under the kind's ClaimTTL. With ClaimTTL 0 — the default —
// a claim is never stale, so an executor that dies mid-action wedges the
// request until Release is called.
//
// Claim is not idempotent for the current holder: retrying after an
// ambiguous response (e.g. a timeout that hid a successful write) returns
// ErrAlreadyClaimed rather than reconfirming the caller's own claim, so
// that error alone does not mean another executor has it.
func (m *Manager) Claim(ctx context.Context, reqID id.UUID, executor string) (Request, error) {
	if executor == "" {
		return Request{}, ErrExecutorRequired
	}
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		switch r.Status {
		case Approved:
			// claimable
		case Executing:
			pol, ok := m.policyFor(r.Kind)
			if !ok {
				return ErrUnknownKind
			}
			if pol.ClaimTTL <= 0 {
				return ErrAlreadyClaimed
			}
			if m.now().Before(r.ClaimedAt.Add(pol.ClaimTTL)) {
				return ErrAlreadyClaimed
			}
		case Expired:
			return ErrExpired
		default:
			return ErrNotApproved
		}
		r.Status = Executing
		r.ClaimedBy = executor
		r.ClaimedAt = m.now()
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionClaim, executor, outcomeSuccess, "")
}

// Complete records that the claimed action succeeded. Only the executor
// holding the claim may call it.
func (m *Manager) Complete(ctx context.Context, reqID id.UUID, executor string) (Request, error) {
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if err := checkHolder(*r, executor); err != nil {
			return err
		}
		r.Status = Executed
		r.DecidedAt = m.now()
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionComplete, executor, outcomeSuccess, "")
}

// Fail records that the claimed action failed, storing reason under the
// "failure" meta key. Only the claim holder may call it. The request is
// terminal afterwards: re-running it means submitting a new request, which
// means asking for approval again.
func (m *Manager) Fail(ctx context.Context, reqID id.UUID, executor, reason string) (Request, error) {
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if err := checkHolder(*r, executor); err != nil {
			return err
		}
		r.Status = Failed
		r.DecidedAt = m.now()
		if r.Meta == nil {
			r.Meta = make(map[string]string, 1)
		}
		r.Meta["failure"] = reason
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionFail, executor, outcomeFailure, reason)
}

// Release returns a claimed request to Approved so another executor can
// take it. It is the administrative escape hatch for a request wedged by a
// dead executor, so it is deliberately NOT holder-checked — the holder is
// precisely the party that cannot call it. Gate access to it in your own
// application: releasing a live executor's claim lets another executor
// re-claim and re-run the action — genuine double execution — and the true
// holder's later Complete then returns ErrNotExecuting, silently discarding
// a successful completion. Every call is audited.
func (m *Manager) Release(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	if a.Subject.ID == "" {
		return Request{}, ErrActorRequired
	}
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if r.Status != Executing {
			return ErrNotExecuting
		}
		r.Status = Approved
		r.ClaimedBy = ""
		r.ClaimedAt = time.Time{}
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionRelease, a.Subject.ID, outcomeSuccess, a.Reason)
}

// Execute claims the request, runs fn, and reports the outcome — the
// Claim/Complete/Fail trio wired correctly so that forgetting the failure
// path cannot wedge a request.
//
// fn receives the claimed request; decode its payload with PayloadOf. A
// non-nil fn error transitions the request to Failed and is returned joined
// with any transition error. ErrAlreadyClaimed is returned unwrapped when
// another executor holds the claim, and fn never runs.
//
// A claim that is durable but whose own audit write failed is not a claim
// failure: fn still runs against the claimed Request, and Complete or Fail
// still applies normally. The returned error is joined with ErrAuditFailed
// so the gap in the trail stays visible via errors.Is instead of being
// silently swallowed by treating the claim as if it had never happened.
//
// fn runs exactly once per successful claim. Under a non-zero ClaimTTL a
// stale claim can be taken over, so fn may run more than once across
// executor deaths — make it idempotent, or leave ClaimTTL at 0.
//
// There is no panic recovery: if fn panics, the request stays wedged in
// Executing (consistent with the crashed-executor philosophy — Release or
// a ClaimTTL takeover is what recovers it).
func (m *Manager) Execute(ctx context.Context, reqID id.UUID, executor string, fn func(context.Context, Request) error) (Request, error) {
	r, claimErr := m.Claim(ctx, reqID, executor)
	if claimErr != nil && !errors.Is(claimErr, ErrAuditFailed) {
		return Request{}, claimErr
	}
	if err := fn(ctx, r); err != nil {
		failed, ferr := m.Fail(ctx, reqID, executor, err.Error())
		return failed, errors.Join(err, ferr, claimErr)
	}
	completed, err := m.Complete(ctx, reqID, executor)
	return completed, errors.Join(err, claimErr)
}

// checkHolder gates Complete and Fail on the claim.
func checkHolder(r Request, executor string) error {
	if r.Status != Executing {
		return ErrNotExecuting
	}
	if r.ClaimedBy != executor {
		return ErrNotClaimHolder
	}
	return nil
}
