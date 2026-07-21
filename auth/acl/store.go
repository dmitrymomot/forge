package acl

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
)

// Store persists ACL entries. Tenant is explicit at the boundary
// (single-tenant callers pass ""). The entry key is (tenant, subject,
// resource type, resource id, action); Put upserts by that key, so
// re-granting a denied key (or vice versa) flips its effect in place.
// EntriesFor may over-return (any superset of the matching entries is legal —
// the Decider re-filters); it must never omit a matching entry.
// Implementations must be safe for concurrent use.
type Store interface {
	// EntriesFor returns subject's entries on (resourceType, resourceID),
	// including the type-wide entries (ResourceID "").
	EntriesFor(ctx context.Context, tenant, subject, resourceType, resourceID string) ([]Entry, error)
	// ListFor returns all of subject's entries within tenant — the admin
	// "what exactly can this subject touch" surface.
	ListFor(ctx context.Context, tenant, subject string) ([]Entry, error)
	// Put upserts entries by key (idempotent). It rejects an entry whose
	// Effect is neither Allow nor Deny (ErrInvalidEntry) without writing
	// anything.
	Put(ctx context.Context, tenant string, entries []Entry) error
	// Delete removes subject's entries on the resource for the given actions,
	// whatever their effect. Missing keys are not an error.
	Delete(ctx context.Context, tenant, subject, resourceType, resourceID string, actions []string) error
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

// Manager is the tenant-scoped admin surface over a Store: grant, deny,
// revoke, and list a subject's entries.
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

// Grant writes Allow entries: subject may perform each action on the resource
// (resourceID "" = type-wide, action "*" = every action). Overwrites a deny
// on the same key.
func (m *Manager) Grant(ctx context.Context, subject, resourceType, resourceID string, actions ...string) error {
	return m.put(ctx, access.Allow, subject, resourceType, resourceID, actions)
}

// Deny writes Deny entries: subject may not perform each action on the
// resource, vetoing any role grant. Overwrites a grant on the same key.
func (m *Manager) Deny(ctx context.Context, subject, resourceType, resourceID string, actions ...string) error {
	return m.put(ctx, access.Deny, subject, resourceType, resourceID, actions)
}

func (m *Manager) put(ctx context.Context, effect access.Effect, subject, resourceType, resourceID string, actions []string) error {
	if len(actions) == 0 {
		return nil
	}
	if subject == "" {
		return fmt.Errorf("%w: empty subject", ErrInvalidEntry)
	}
	if resourceType == "" {
		return fmt.Errorf("%w: empty resource type", ErrInvalidEntry)
	}
	entries := make([]Entry, 0, len(actions))
	for _, a := range actions {
		if a == "" {
			return fmt.Errorf("%w: empty action", ErrInvalidEntry)
		}
		entries = append(entries, Entry{
			Subject:      subject,
			ResourceType: resourceType,
			ResourceID:   resourceID,
			Action:       a,
			Effect:       effect,
		})
	}
	t, err := m.tenant(ctx)
	if err != nil {
		return err
	}
	return m.store.Put(ctx, t, entries)
}

// Revoke removes subject's entries on the resource for the given actions,
// grants and denies alike — the resource falls back to role decisions.
func (m *Manager) Revoke(ctx context.Context, subject, resourceType, resourceID string, actions ...string) error {
	if len(actions) == 0 {
		return nil
	}
	t, err := m.tenant(ctx)
	if err != nil {
		return err
	}
	return m.store.Delete(ctx, t, subject, resourceType, resourceID, actions)
}

// ListFor returns all of subject's entries within the resolved tenant.
func (m *Manager) ListFor(ctx context.Context, subject string) ([]Entry, error) {
	t, err := m.tenant(ctx)
	if err != nil {
		return nil, err
	}
	return m.store.ListFor(ctx, t, subject)
}
