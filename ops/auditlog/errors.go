package auditlog

import "errors"

var (
	// ErrInvalidEvent rejects Record calls with an empty Action or Outcome —
	// an audit record must state what happened and how it ended.
	ErrInvalidEvent = errors.New("auditlog: invalid event")

	// ErrScope fails Record closed when the WithScope hook errors or
	// yields an empty tenant.
	ErrScope = errors.New("auditlog: tenant scope unavailable")

	// ErrTenantMismatch rejects events whose explicit Tenant conflicts
	// with the WithScope-derived tenant.
	ErrTenantMismatch = errors.New("auditlog: tenant mismatch")

	// ErrChainBroken reports a tampered or gapped hash chain: an event
	// whose PrevHash does not extend the chain head or whose Hash does not
	// match its recomputed value.
	ErrChainBroken = errors.New("auditlog: chain broken")
)
