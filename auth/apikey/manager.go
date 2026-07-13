package apikey

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Manager issues, manages, and verifies API keys over a Store. It
// implements guard.Verifier. Safe for concurrent use.
type Manager struct {
	store Store
	cfg   config
}

// New builds a Manager. It panics on a nil store or an invalid prefix —
// wiring bugs caught at startup, like guard.New's nil-verifier panic.
func New(store Store, opts ...Option) *Manager {
	if store == nil {
		panic("apikey: nil store")
	}
	cfg := config{prefix: "key", touchInterval: time.Minute}
	for _, o := range opts {
		o(&cfg)
	}
	if !validPrefix(cfg.prefix) {
		panic(fmt.Sprintf("apikey: invalid prefix %q", cfg.prefix))
	}
	return &Manager{store: store, cfg: cfg}
}

// Create mints a key for p, returning the stored record and the plaintext.
// The plaintext is shown exactly once — only its hash is persisted.
func (m *Manager) Create(ctx context.Context, p CreateParams) (Key, string, error) {
	if p.Subject == "" {
		return Key{}, "", ErrSubjectRequired
	}
	tenant, err := m.scoped(ctx, p.Tenant)
	if err != nil {
		return Key{}, "", err
	}
	plaintext := newKey(m.cfg.prefix)
	k := Key{
		ID:        id.NewUUID(),
		Hash:      hashKey(plaintext),
		Preview:   plaintext[:previewLen],
		Name:      p.Name,
		Subject:   p.Subject,
		Tenant:    tenant,
		Scopes:    slices.Clone(p.Scopes),
		Meta:      maps.Clone(p.Meta),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: p.ExpiresAt,
	}
	if err := m.store.Create(ctx, k); err != nil {
		return Key{}, "", fmt.Errorf("apikey: create: %w", err)
	}
	return k, plaintext, nil
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

// Get returns one key record. With WithScope configured, other tenants'
// keys read as ErrNotFound so existence cannot be probed across tenants.
func (m *Manager) Get(ctx context.Context, keyID id.UUID) (Key, error) {
	tenant, err := m.scoped(ctx, "")
	if err != nil {
		return Key{}, err
	}
	k, err := m.store.Get(ctx, keyID)
	if err != nil {
		return Key{}, err
	}
	if m.cfg.scope != nil && k.Tenant != tenant {
		return Key{}, ErrNotFound
	}
	return k, nil
}

// List returns keys matching f, newest first. With WithScope configured
// the filter is confined to the scoped tenant.
func (m *Manager) List(ctx context.Context, f Filter) ([]Key, error) {
	tenant, err := m.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, err
	}
	f.Tenant = tenant
	return m.store.List(ctx, f)
}

// Revoke permanently disables a key. Revocation is terminal — a revoked
// key cannot be un-revoked or rotated.
func (m *Manager) Revoke(ctx context.Context, keyID id.UUID) error {
	if _, err := m.Get(ctx, keyID); err != nil {
		return err
	}
	return m.store.Revoke(ctx, keyID, time.Now().UTC())
}

// Rotate mints a replacement inheriting the old key's Subject, Tenant,
// Scopes, Name, and Meta (not its expiry), and expires the old key after
// grace (zero = immediate cutover). Both keys verify during the grace
// window. The replacement is created before the old key is expired so a
// failure cannot leave the caller without a working key.
func (m *Manager) Rotate(ctx context.Context, keyID id.UUID, grace time.Duration) (Key, string, error) {
	old, err := m.Get(ctx, keyID)
	if err != nil {
		return Key{}, "", err
	}
	now := time.Now().UTC()
	if !old.RevokedAt.IsZero() {
		return Key{}, "", ErrKeyRevoked
	}
	if !old.ExpiresAt.IsZero() && !old.ExpiresAt.After(now) {
		return Key{}, "", ErrKeyExpired
	}
	replacement, plaintext, err := m.Create(ctx, CreateParams{
		Name:    old.Name,
		Subject: old.Subject,
		Tenant:  old.Tenant,
		Scopes:  old.Scopes,
		Meta:    old.Meta,
	})
	if err != nil {
		return Key{}, "", err
	}
	if err := m.store.Expire(ctx, old.ID, now.Add(grace)); err != nil {
		return Key{}, "", fmt.Errorf("apikey: rotate: %w", err)
	}
	return replacement, plaintext, nil
}
