package approval

import (
	"fmt"
	"time"
)

// Policy is the per-kind rule set. It is registered at construction with
// WithKind and never supplied by the caller at submit time: a caller who
// could choose the quorum could weaken the gate on the action they are
// asking permission for.
type Policy struct {
	// TTL is how long a request stays actionable after submission. Zero
	// means it never expires.
	TTL time.Duration
	// ClaimTTL is how long an executor's claim is held before another
	// executor may take it over. Zero — the default — means the claim never
	// expires: an executor that dies mid-action wedges the request until
	// Release is called. That is the safe default for actions that move
	// money. Setting it non-zero opts into at-least-once execution, so the
	// action behind it must be idempotent.
	ClaimTTL time.Duration
	// Quorum is the number of distinct approvals required. Must be at
	// least 1. Note that Quorum 1 is still dual control: the requester can
	// never be the approver.
	Quorum int
}

// validate reports why a policy is unusable, or nil.
func (p Policy) validate(kind string) error {
	if p.Quorum < 1 {
		return fmt.Errorf("approval: kind %q: quorum must be >= 1, got %d", kind, p.Quorum)
	}
	if p.TTL < 0 {
		return fmt.Errorf("approval: kind %q: negative TTL %s", kind, p.TTL)
	}
	if p.ClaimTTL < 0 {
		return fmt.Errorf("approval: kind %q: negative ClaimTTL %s", kind, p.ClaimTTL)
	}
	return nil
}
