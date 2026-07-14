package rbac

import (
	"context"
	"fmt"
)

// Store persists subject→role assignments. Tenant is explicit at the boundary
// (single-tenant callers pass ""). Implementations must be safe for concurrent
// use and idempotent on Assign.
type Store interface {
	RolesFor(ctx context.Context, tenant, subject string) ([]string, error)
	Assign(ctx context.Context, tenant, subject string, roles []string) error
	Unassign(ctx context.Context, tenant, subject string, roles []string) error
}

type managerConfig struct {
	scope func(context.Context) (string, error)
}

// Option configures a Manager.
type Option func(*managerConfig)

// WithScope derives the tenant from context for every Manager operation and
// fails closed (ErrScope) when the hook errors or yields an empty tenant. A nil
// fn (or no WithScope) leaves the Manager single-tenant, operating on "".
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *managerConfig) { c.scope = fn }
}

// Manager is the tenant-scoped admin surface over a Store: assign, unassign,
// and read a subject's roles.
type Manager struct {
	store Store
	cfg   managerConfig
}

// NewManager wraps store with the given options.
func NewManager(store Store, opts ...Option) *Manager {
	var cfg managerConfig
	for _, o := range opts {
		o(&cfg)
	}
	return &Manager{store: store, cfg: cfg}
}

// tenant resolves the scope for this operation. No hook -> "" (single-tenant).
func (m *Manager) tenant(ctx context.Context) (string, error) {
	if m.cfg.scope == nil {
		return "", nil
	}
	t, err := m.cfg.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if t == "" {
		return "", fmt.Errorf("%w: empty tenant", ErrScope)
	}
	return t, nil
}

// Assign grants roles to subject (idempotent).
func (m *Manager) Assign(ctx context.Context, subject string, roles ...string) error {
	t, err := m.tenant(ctx)
	if err != nil {
		return err
	}
	return m.store.Assign(ctx, t, subject, roles)
}

// Unassign revokes roles from subject.
func (m *Manager) Unassign(ctx context.Context, subject string, roles ...string) error {
	t, err := m.tenant(ctx)
	if err != nil {
		return err
	}
	return m.store.Unassign(ctx, t, subject, roles)
}

// RolesFor returns subject's assigned roles within the resolved tenant.
func (m *Manager) RolesFor(ctx context.Context, subject string) ([]string, error) {
	t, err := m.tenant(ctx)
	if err != nil {
		return nil, err
	}
	return m.store.RolesFor(ctx, t, subject)
}
