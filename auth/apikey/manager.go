package apikey

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Manager holds validated settings and nothing else — no storage handle and
// no mutable state, so one Manager serves every goroutine. Each operation
// takes the storage effects it performs as arguments, so a call site can
// supply closures over its own transaction.
type Manager struct {
	cfg config
}

// New validates opts into a Manager. Options accumulate their problems, so
// one call reports every bad value; the joined error matches ErrConfig.
func New(opts ...Option) (*Manager, error) {
	cfg := config{prefix: "key", touchInterval: time.Minute}
	for _, o := range opts {
		o(&cfg)
	}
	if len(cfg.errs) > 0 {
		return nil, errors.Join(cfg.errs...)
	}
	cfg.valid = true
	return &Manager{cfg: cfg}, nil
}

// settings returns the validated settings an operation runs under, or
// reports why it cannot start. Manager is exported so
// callers can hold one, which also makes the zero value constructible — a
// Manager that did not come from New reports ErrConfig rather than issuing
// keys under an empty prefix, and so does the nil one New returns beside an
// error. A missing storage effect is named, because a call site builds its
// closures from request-scoped values and a nil one is a wiring error, not
// a panic.
func (m *Manager) settings(effect string, missing bool) (config, error) {
	if m == nil || !m.cfg.valid {
		return config{}, ErrConfig
	}
	if missing {
		return config{}, fmt.Errorf("%w: %s", ErrNilEffect, effect)
	}
	return m.cfg, nil
}

// scoped resolves the tenant a management operation is confined to. With
// no WithScope hook it passes the requested tenant through. Fail-closed:
// hook errors and empty scoped tenants abort the operation with ErrScope;
// an explicit requested tenant must be empty or equal to the scoped one.
func (c config) scoped(ctx context.Context, requested string) (string, error) {
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

// touchDue reports whether a record's last-used stamp is stale enough to
// rewrite. A negative interval disables tracking.
func (c config) touchDue(lastUsed, now time.Time) bool {
	if c.touchInterval < 0 {
		return false
	}
	return lastUsed.IsZero() || now.Sub(lastUsed) >= c.touchInterval
}
