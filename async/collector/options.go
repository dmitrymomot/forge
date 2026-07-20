package collector

import (
	"context"
	"log/slog"
)

// settings is the non-generic construction state options mutate.
type settings struct {
	log      *slog.Logger
	scope    func(ctx context.Context) (string, error)
	scopeCtx func(ctx context.Context, scope string) context.Context
	name     string
	cfg      Config
}

// Option configures New.
type Option func(*settings)

// WithConfig replaces the Config (validated by New).
func WithConfig(cfg Config) Option {
	return func(s *settings) { s.cfg = cfg }
}

// WithName overrides the supervisor service name (default "collector");
// required when running multiple collectors under one supervisor.
func WithName(name string) Option {
	return func(s *settings) { s.name = name }
}

// WithLogger sets the logger (default logger.NewNope()).
func WithLogger(l *slog.Logger) Option {
	return func(s *settings) { s.log = l }
}

// WithScope installs the tenancy hook: it is called on every Add and its
// result is captured into the buffered event. Fail-closed: once configured, a
// hook error or empty scope makes Add fail with ErrScopeMissing.
// Single-tenant apps simply do not configure it.
func WithScope(fn func(ctx context.Context) (string, error)) Option {
	return func(s *settings) { s.scope = fn }
}

// WithScopeContext installs the tenancy restore hook: flushes are partitioned
// by captured scope and the hook is called once per scoped batch; the
// returned context is the Flush call's base context. Without it the sink
// receives a plain context (single-tenant, or a sink that does not need the
// scope restored).
func WithScopeContext(fn func(ctx context.Context, scope string) context.Context) Option {
	return func(s *settings) { s.scopeCtx = fn }
}
