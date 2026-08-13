package apikey

import (
	"context"
	"fmt"
	"time"
)

// Config is the validated, stateless settings value every operation takes.
// Build it once at wiring with NewConfig and pass it by value — it holds no
// storage and no mutable state, so one Config serves every goroutine. Only
// NewConfig marks a Config valid, so a value from anywhere else (the zero
// value included) is rejected with ErrConfig by every operation.
type Config struct {
	scope         func(context.Context) (string, error)
	prefix        string
	touchInterval time.Duration
	valid         bool
}

// Option configures NewConfig.
type Option func(*Config)

// NewConfig validates opts into a Config. It reports ErrConfig for an
// invalid prefix.
func NewConfig(opts ...Option) (Config, error) {
	cfg := Config{prefix: "key", touchInterval: time.Minute}
	for _, o := range opts {
		o(&cfg)
	}
	if !validPrefix(cfg.prefix) {
		return Config{}, fmt.Errorf("%w: invalid prefix %q", ErrConfig, cfg.prefix)
	}
	cfg.valid = true
	return cfg, nil
}

// WithPrefix sets the key prefix (default "key"); keys read
// <prefix>_<payload><checksum>. The prefix must match [a-z0-9_]+.
// Environments are separate issuers: build one Config with "sk_live" and
// another with "sk_test".
func WithPrefix(p string) Option {
	return func(c *Config) { c.prefix = p }
}

// WithScope derives the tenant from context for every management
// operation: Create stamps it, List is confined to it, and
// Get/Revoke/Rotate report ErrNotFound for other tenants' keys.
// Fail-closed: a hook error or empty tenant fails the operation with
// ErrScope. Verify is unaffected — the key record itself carries the
// tenant. A nil fn leaves the Config unscoped.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *Config) { c.scope = fn }
}

// WithTouchInterval throttles last-used-at writes: Verify calls its
// TouchFunc only when LastUsedAt is staler than d (default 60s). Zero
// touches on every request; negative disables tracking.
func WithTouchInterval(d time.Duration) Option {
	return func(c *Config) { c.touchInterval = d }
}

// ready reports why an operation cannot start: an unvalidated Config, or a
// missing storage effect named by effect. One guard per operation keeps the
// two checks in a fixed order everywhere.
func (c Config) ready(effect string, missing bool) error {
	if !c.valid {
		return ErrConfig
	}
	if missing {
		return fmt.Errorf("%w: %s", ErrNilEffect, effect)
	}
	return nil
}

// scoped resolves the tenant a management operation is confined to. With
// no WithScope hook it passes the requested tenant through. Fail-closed:
// hook errors and empty scoped tenants abort the operation with ErrScope;
// an explicit requested tenant must be empty or equal to the scoped one.
func (c Config) scoped(ctx context.Context, requested string) (string, error) {
	if c.scope == nil {
		return requested, nil
	}
	t, err := c.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if t == "" {
		return "", ErrScope
	}
	if requested != "" && requested != t {
		return "", ErrTenantMismatch
	}
	return t, nil
}
