package auditlog

import (
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Outcome classifies how an audited action ended. The constants cover the
// common cases; free-form values are allowed for domain-specific outcomes.
type Outcome string

// Common outcomes.
const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeDenied  Outcome = "denied"
)

// Event is one append-only audit record: who (Actor) did what (Action) to
// which target (Resource) with what result (Outcome). Action and Outcome
// are required; everything else is optional context.
//
// ID and Time are assigned by Recorder.Record — ID is always a fresh
// time-ordered UUIDv7, and Time defaults to the recorder's clock when zero
// (both truncated to microseconds so records survive a Postgres
// round-trip). PrevHash and Hash are populated only by a WithChain
// recorder and link each event to the previous one in its stream.
type Event struct {
	// Time is when the action occurred, UTC, microsecond precision.
	Time time.Time `json:"time"`
	// Meta carries free-form context (request id, changed fields, reason).
	Meta map[string]string `json:"meta,omitempty"`
	// Tenant is the owning tenant; empty in single-tenant applications.
	Tenant string `json:"tenant,omitempty"`
	// Actor is the principal that performed the action (user id, API key
	// id, or a service name for system actions).
	Actor string `json:"actor,omitempty"`
	// Action names what happened, dot-scoped by convention ("user.invite").
	Action string `json:"action"`
	// Resource identifies the target ("user:42", "invoice:2024-001").
	Resource string `json:"resource,omitempty"`
	// Outcome is the result of the action.
	Outcome Outcome `json:"outcome"`
	// PrevHash is the Hash of the previous event in this stream ("" for
	// the first event). Set by a WithChain recorder.
	PrevHash string `json:"prev_hash,omitempty"`
	// Hash is the SHA-256 chain hash of this event. Set by a WithChain
	// recorder.
	Hash string `json:"hash,omitempty"`
	// ID is a time-ordered UUIDv7 assigned by Record.
	ID id.UUID `json:"id"`
}
