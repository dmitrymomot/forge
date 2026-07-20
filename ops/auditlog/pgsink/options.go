package pgsink

import "context"

// Option configures New.
type Option func(*Sink)

// WithScope derives the tenant from context for every read (List, Verify,
// ChainHead), confining queries to that tenant. Fail-closed: a hook error
// or empty tenant fails the call with auditlog.ErrScope, and an explicit
// tenant that disagrees with the hook fails with
// auditlog.ErrTenantMismatch. Write is unaffected — the recorder's own
// WithScope stamps the tenant onto each event before it reaches the sink;
// a chained recorder over a scoped sink must therefore use the same hook,
// since its chain seeding reads ChainHead. A nil fn leaves reads
// unscoped.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(s *Sink) { s.scope = fn }
}
