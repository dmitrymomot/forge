// Package auditlog records append-only structured audit events — who
// (Actor) did what (Action) to which target (Resource) with what result
// (Outcome) — through a pluggable Sink. SlogSink (observability) and
// JSONLSink (file trail) ship here; a database-backed Sink implemented by
// the consumer adds the tenant-isolated, keyset-paginated queries an
// audit-trail UI needs.
//
//	sink := auditlog.NewMemorySink() // a durable Sink in production
//	rec := auditlog.New(sink)
//
//	e, err := rec.Record(ctx, auditlog.Event{
//		Actor:    "user_42",
//		Action:   "member.invite",
//		Resource: "member:bob@example.com",
//		Outcome:  auditlog.OutcomeSuccess,
//		Meta:     map[string]string{"role": "admin"},
//	})
//	// e.ID is the audit reference to surface to the user.
//
// Record stamps a time-ordered UUIDv7 ID and the current time, then
// writes synchronously: a sink error is the caller's error, never a
// silent drop. Multi-tenant applications stamp the tenant with the
// fail-closed WithScope hook:
//
//	rec := auditlog.New(sink, auditlog.WithScope(func(ctx context.Context) (string, error) {
//		return tenantFromCtx(ctx), nil // empty or error aborts Record
//	}))
//
// # Tamper evidence
//
// WithChain links each event to its predecessor within a stream (stream =
// tenant): Hash = SHA-256(PrevHash, payload). Rewriting, deleting, or
// reordering any persisted event breaks every hash after it, which
// VerifyChain detects. The chain is unkeyed, so an attacker who can
// rewrite the entire suffix after an edit — or truncate the tail —
// defeats it; anchoring the verified head outside the database closes
// that gap. Chained writes serialize per stream and require a single
// writer per stream; sinks implementing ChainHead (MemorySink, or a
// database-backed sink) let the chain resume across restarts.
//
//	rec := auditlog.New(sink, auditlog.WithChain())
//	head, err := auditlog.VerifyChain("", events) // events in id-ascending order
package auditlog
