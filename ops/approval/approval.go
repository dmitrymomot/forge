package approval

import (
	"encoding/json"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/id"
)

// Status is the lifecycle state of an approval request.
type Status uint8

// Request statuses. Expired is derived on read from ExpiresAt and is never
// persisted — see Manager.Get.
const (
	Pending   Status = iota // awaiting decisions
	Approved                // quorum met, awaiting claim
	Rejected                // a checker rejected it
	Cancelled               // withdrawn before execution
	Expired                 // TTL elapsed while Pending or Approved
	Executing               // claimed by an executor
	Executed                // executor reported success
	Failed                  // executor reported failure
)

// String renders the status for logs, audit metadata, and errors.
func (s Status) String() string {
	switch s {
	case Pending:
		return "pending"
	case Approved:
		return "approved"
	case Rejected:
		return "rejected"
	case Cancelled:
		return "cancelled"
	case Expired:
		return "expired"
	case Executing:
		return "executing"
	case Executed:
		return "executed"
	case Failed:
		return "failed"
	default:
		return "unknown"
	}
}

// Terminal reports whether the status admits no further transitions.
func (s Status) Terminal() bool {
	switch s {
	case Rejected, Cancelled, Expired, Executed, Failed:
		return true
	default:
		return false
	}
}

// Vote is the direction of a checker's decision.
type Vote uint8

// Vote directions. The zero value is invalid so an unset Vote cannot pass
// for an approval.
const (
	VoteApprove Vote = iota + 1
	VoteReject
)

// String renders the vote for audit metadata.
func (v Vote) String() string {
	switch v {
	case VoteApprove:
		return "approve"
	case VoteReject:
		return "reject"
	default:
		return "unknown"
	}
}

// Kind binds an approval action name to its payload type T. Declare one
// package-level Kind per action and share it between the code that submits
// and the code that executes: the name exists in exactly one place, and
// payload type drift becomes a compile error.
//
//	var KindReleasePayout = approval.NewKind[ReleasePayout]("payout.release")
type Kind[T any] struct {
	name string
}

// NewKind creates a Kind for payload type T. Panics on an empty name:
// kinds are package-level wiring, not runtime data.
func NewKind[T any](name string) Kind[T] {
	if name == "" {
		panic("approval: NewKind requires a non-empty name")
	}
	return Kind[T]{name: name}
}

// Name returns the action name.
func (k Kind[T]) Name() string { return k.name }

// Decision is one checker's vote on a request. Decisions are append-only.
type Decision struct {
	// At is when the decision was cast, UTC, microsecond precision.
	At time.Time `json:"at"`
	// Approver is the deciding subject's id.
	Approver string `json:"approver"`
	// Reason is the checker's free-form justification.
	Reason string `json:"reason,omitempty"`
	// Vote is the direction of the decision.
	Vote Vote `json:"vote"`
}

// Request is one privileged action awaiting dual control.
type Request struct {
	// CreatedAt is when the request was submitted.
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is when the request stops being actionable. Zero means it
	// never expires.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// ClaimedAt is when the current executor claimed it. Zero when
	// unclaimed.
	ClaimedAt time.Time `json:"claimed_at,omitzero"`
	// DecidedAt is when the request reached Approved or a terminal status.
	DecidedAt time.Time `json:"decided_at,omitzero"`
	// Meta carries free-form submitter context.
	Meta map[string]string `json:"meta,omitempty"`
	// Kind is the registered action name.
	Kind string `json:"kind"`
	// Tenant is the owning tenant; empty in single-tenant applications.
	Tenant string `json:"tenant,omitempty"`
	// Requester is the maker — the principal that submitted the request.
	Requester string `json:"requester"`
	// Reason is the maker's justification, shown to checkers.
	Reason string `json:"reason,omitempty"`
	// ClaimedBy is the executor currently holding the claim.
	ClaimedBy string `json:"claimed_by,omitempty"`
	// Decisions holds every vote cast, in the order cast.
	Decisions []Decision `json:"decisions,omitempty"`
	// Payload is the JSON-encoded action payload. Decode it with PayloadOf.
	Payload json.RawMessage `json:"payload"`
	// Version is the optimistic-concurrency counter. Store.Update accepts a
	// write only when the stored value matches.
	Version int64 `json:"version"`
	// ID is a time-ordered UUIDv7 assigned at submit.
	ID id.UUID `json:"id"`
	// Status is the lifecycle state.
	Status Status `json:"status"`
}

// Approvals counts the approving votes cast so far.
func (r Request) Approvals() int {
	n := 0
	for i := range r.Decisions {
		if r.Decisions[i].Vote == VoteApprove {
			n++
		}
	}
	return n
}

// SubmitParams carries the maker's side of a submission.
type SubmitParams struct {
	// Meta is free-form context, cloned on submit.
	Meta map[string]string
	// Requester is the maker's principal id. Required.
	Requester string
	// Tenant is optional; under WithScope it must be empty or equal to the
	// scoped tenant.
	Tenant string
	// Reason is the maker's justification, persisted on the request.
	Reason string
}

// Actor is the human acting on a request — a checker casting a decision, or
// the party cancelling. It carries an access.Subject rather than a bare id
// so a decider has real attributes to judge on. Executors are machines and
// pass a plain executor id instead.
type Actor struct {
	Reason  string
	Subject access.Subject
}

// Filter selects requests for List.
type Filter struct {
	// ExpiresBefore bounds ExpiresAt, selecting requests that have expired
	// or are about to. Zero means no bound. Requests with no expiry are
	// never matched by it.
	ExpiresBefore time.Time
	// Kind, Tenant, and Requester are exact matches; empty means "any".
	Kind      string
	Tenant    string
	Requester string
	// Statuses matches the STORED status. Expired is never stored (it is
	// derived on read), so listing it matches nothing — query
	// []Status{Pending, Approved} with ExpiresBefore instead.
	Statuses []Status
	// Limit caps the number of records returned. Zero defaults to 100; every
	// Store implementation must apply the same default so swapping stores
	// cannot silently truncate results.
	Limit int
}
