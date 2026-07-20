package invoice

import "context"

type sequenceConfig struct {
	scope  func(context.Context) (string, error)
	format func(series string, n int64) string
	mode   NumberingMode
}

// Option configures NewSequence.
type Option func(*sequenceConfig)

// WithMode sets the numbering mode. Default Gapless.
func WithMode(m NumberingMode) Option {
	return func(c *sequenceConfig) { c.mode = m }
}

// WithFormat sets the display format applied to a drawn counter value, e.g.
// yearly series "INV-2026" with the default format render "INV-2026-000042".
// The formatter receives the caller-visible series, never the tenant-scoped
// storage key. Default "SERIES-000042".
func WithFormat(fn func(series string, n int64) string) Option {
	return func(c *sequenceConfig) { c.format = fn }
}

// WithScope derives a tenant scope from the request context on every number
// draw, so multi-tenant isolation is wired once at construction instead of at
// every call site: each tenant gets independent per-series counters while
// formatted numbers stay tenant-clean. The hook fails closed: an error or
// empty scope aborts the draw with ErrScope — one tenant's series can never
// increment another tenant's (or a global) counter. Single-tenant
// applications omit this option.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *sequenceConfig) { c.scope = fn }
}
