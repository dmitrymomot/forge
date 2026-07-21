package approval

import (
	"context"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

type config struct {
	clk        clock.Clock
	decider    access.Decider
	auditor    *auditlog.Recorder
	scope      func(context.Context) (string, error)
	kinds      map[string]Policy
	maxRetries int
}

// Option configures New.
type Option func(*config)

// WithKind registers an action name and the policy that governs it. It is
// the only way a kind enters a Manager — the registry is immutable after
// New, so the read path needs no lock and no caller can register a weaker
// policy at runtime. Repeat it once per action.
//
// New panics on a duplicate name, a quorum below 1, or a negative duration.
func WithKind[T any](k Kind[T], p Policy) Option {
	return func(c *config) {
		name := k.Name()
		if _, dup := c.kinds[name]; dup {
			panic("approval: duplicate kind " + name)
		}
		if err := p.validate(name); err != nil {
			panic(err.Error())
		}
		c.kinds[name] = p
	}
}

// WithClock injects a clock for deterministic tests. Defaults to
// clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithMaxRetries caps how many times a transition re-reads and retries
// after losing a version race (default 3). Each retry re-validates from a
// fresh read, so a retry can still legitimately fail with ErrAlreadyVoted.
// Values below zero are clamped to zero (a single attempt).
func WithMaxRetries(n int) Option {
	return func(c *config) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}

// WithDecider gates who may decide on a request. The manager asks it
// "<kind>:decide" before recording any vote and "<kind>:cancel" before a
// non-requester cancellation, passing the request's kind, requester, and
// raw payload as resource attributes so relational rules ("must be the
// requester's manager") and value-aware rules ("over 10 days needs the
// department head") both work.
//
// The decider may be consulted more than once per call: eligible() re-runs
// on every mutate CAS retry, so a decider that hits a database is called
// once per retry, not once per Approve/Reject/Cancel. Do not build a rate
// limiter or any other side-effecting hook into a decider.
//
// Without it, any principal other than the requester may decide — the
// correct single-team default. The structural invariants (no self-approval,
// one vote per approver) hold either way.
func WithDecider(d access.Decider) Option {
	return func(c *config) { c.decider = d }
}

// WithAuditor records every state change to the audit trail: submissions,
// decisions, cancellations, claims, and outcomes — plus every refused
// attempt to influence a decision, as OutcomeDenied: decider denials,
// self-approvals, and duplicate votes.
//
// The trail is written after the state change is durable. If the sink
// fails, the transition still happened and the operation returns
// ErrAuditFailed alongside the updated request.
func WithAuditor(rec *auditlog.Recorder) Option {
	return func(c *config) { c.auditor = rec }
}

// WithScope derives the tenant from context for every operation: Submit
// stamps it, List is confined to it, and Get and every transition report
// ErrNotFound for other tenants' requests — not a forbidden error, so
// cross-tenant existence cannot be probed.
//
// Fail-closed: a hook error or an empty tenant fails the operation with
// ErrScope. A nil fn leaves the manager unscoped, which is the correct
// single-tenant default.
//
// A disagreement between the requested and scoped tenant wraps ErrScope
// with text naming both tenant ids. That text is diagnostic-only — match
// the error with errors.Is and return callers something opaque, not the
// wrapped message.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}
