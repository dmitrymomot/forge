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
	return &Manager{cfg: cfg}, nil
}

// requireEffect reports a missing storage effect by name. A call site builds
// its closures from request-scoped values, so a nil one is an error rather
// than a panic.
func requireEffect(name string, missing bool) error {
	if missing {
		return fmt.Errorf("%w: %s", ErrNilEffect, name)
	}
	return nil
}

// scoped resolves the tenant a management operation is confined to. With
// no WithScope hook it passes the requested tenant through. Fail-closed:
// hook errors and empty scoped tenants abort the operation with ErrScope;
// an explicit requested tenant must be empty or equal to the scoped one.
func (m *Manager) scoped(ctx context.Context, requested string) (string, error) {
	if m.cfg.scope == nil {
		return requested, nil
	}
	t, err := m.cfg.scope(ctx)
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
func (m *Manager) touchDue(lastUsed, now time.Time) bool {
	if m.cfg.touchInterval < 0 {
		return false
	}
	return lastUsed.IsZero() || now.Sub(lastUsed) >= m.cfg.touchInterval
}
