package approval

import "errors"

var (
	// ErrNotFound is returned by a Store when no request matches, and by
	// Manager operations for other tenants' requests under WithScope (so
	// cross-tenant existence cannot be probed).
	ErrNotFound = errors.New("approval: request not found")

	// ErrDuplicate is returned by Store.Create when the ID already exists.
	ErrDuplicate = errors.New("approval: duplicate request")

	// ErrConflict is returned by Store.Update when the stored Version no
	// longer matches the expected one. The Manager retries on it.
	ErrConflict = errors.New("approval: version conflict")

	// ErrUnknownKind rejects a submission for a kind that was not
	// registered with WithKind.
	ErrUnknownKind = errors.New("approval: unknown kind")

	// ErrKindMismatch rejects PayloadOf when the Kind does not match the
	// request's kind — the payload would decode into the wrong type.
	ErrKindMismatch = errors.New("approval: kind mismatch")

	// ErrRequesterRequired rejects SubmitParams with an empty Requester.
	ErrRequesterRequired = errors.New("approval: requester required")

	// ErrActorRequired rejects a decision from an Actor with an empty
	// Subject.ID — an anonymous checker cannot satisfy dual control.
	ErrActorRequired = errors.New("approval: actor required")

	// ErrExecutorRequired rejects a claim with an empty executor id.
	ErrExecutorRequired = errors.New("approval: executor required")

	// ErrSelfApproval enforces the maker-checker rule: the requester may
	// never decide their own request, at any quorum.
	ErrSelfApproval = errors.New("approval: requester cannot decide own request")

	// ErrAlreadyVoted rejects a second decision from an approver who has
	// already voted, whichever way they voted first.
	ErrAlreadyVoted = errors.New("approval: approver already voted")

	// ErrNotPending rejects a decision on a request that is no longer
	// awaiting one.
	ErrNotPending = errors.New("approval: request not pending")

	// ErrExpired rejects every transition on a request past its TTL.
	ErrExpired = errors.New("approval: request expired")

	// ErrNotApproved rejects a claim on a request that is not in the
	// Approved status: it has not reached quorum yet, or it has already
	// moved past claiming (rejected, cancelled, executed, or failed).
	ErrNotApproved = errors.New("approval: request not approved")

	// ErrAlreadyClaimed rejects a claim held by another executor whose
	// lease has not gone stale.
	ErrAlreadyClaimed = errors.New("approval: request already claimed")

	// ErrNotExecuting rejects Complete, Fail, or Release on a request that
	// is not currently claimed.
	ErrNotExecuting = errors.New("approval: request not executing")

	// ErrNotClaimHolder rejects Complete or Fail from an executor other
	// than the one holding the claim. Release is deliberately exempt.
	ErrNotClaimHolder = errors.New("approval: executor does not hold the claim")

	// ErrNotCancellable rejects Cancel on a request that is executing or
	// has already reached a terminal status other than Expired — an expired
	// request is rejected with ErrExpired instead.
	ErrNotCancellable = errors.New("approval: request not cancellable")

	// ErrNotEligible rejects a decision from an actor the decider denied,
	// or whose eligibility could not be established (fail closed).
	ErrNotEligible = errors.New("approval: actor not eligible")

	// ErrScope is the fail-closed result of a WithScope hook that errored
	// or returned an empty tenant, or of a request tenant that disagrees
	// with the scoped one.
	ErrScope = errors.New("approval: tenant scope unavailable")

	// ErrAuditFailed reports that a transition was persisted but its audit
	// event could not be written. The returned Request is durable; the
	// trail is not. Match it with errors.Is and alert — never swallow it.
	ErrAuditFailed = errors.New("approval: audit write failed")
)
