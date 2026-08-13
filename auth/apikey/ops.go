package apikey

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/redact"
)

// Create mints a key for p and persists it through save, returning the
// stored record and the plaintext. The plaintext is shown exactly once —
// only its hash is persisted — so it comes back wrapped: it renders as
// "REDACTED" through fmt, encoding/json, and log/slog, and Expose is the
// only way to read it. Supply a save closure over the transaction that
// also writes whatever the application stores alongside the key.
func (m *Manager) Create(ctx context.Context, p CreateParams, save SaveFunc) (Key, redact.Secret[string], error) {
	var noPlaintext redact.Secret[string]
	cfg, err := m.settings("save", save == nil)
	if err != nil {
		return Key{}, noPlaintext, err
	}
	if p.Subject == "" {
		return Key{}, noPlaintext, ErrSubjectRequired
	}
	tenant, err := cfg.scoped(ctx, p.Tenant)
	if err != nil {
		return Key{}, noPlaintext, err
	}
	k, plaintext := mint(cfg.prefix, p, tenant)
	if err := save(ctx, k); err != nil {
		return Key{}, noPlaintext, fmt.Errorf("apikey: create: %w", err)
	}
	return k, redact.New(plaintext), nil
}

func mint(prefix string, p CreateParams, tenant string) (Key, string) {
	plaintext := newKey(prefix)
	return Key{
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
	}, plaintext
}

// Get returns one key record. With WithScope configured, other tenants'
// keys read as ErrNotFound so existence cannot be probed across tenants.
func (m *Manager) Get(ctx context.Context, keyID id.UUID, load LoadFunc) (Key, error) {
	cfg, err := m.settings("load", load == nil)
	if err != nil {
		return Key{}, err
	}
	tenant, err := cfg.scoped(ctx, "")
	if err != nil {
		return Key{}, err
	}
	k, err := load(ctx, keyID)
	if err != nil {
		return Key{}, err
	}
	if cfg.scope != nil && k.Tenant != tenant {
		return Key{}, ErrNotFound
	}
	return k, nil
}

// List returns keys matching f, newest first. With WithScope configured
// the filter is confined to the scoped tenant.
func (m *Manager) List(ctx context.Context, f Filter, list ListFunc) ([]Key, error) {
	cfg, err := m.settings("list", list == nil)
	if err != nil {
		return nil, err
	}
	tenant, err := cfg.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, err
	}
	f.Tenant = tenant
	return list(ctx, f)
}

// Revoke permanently disables a key. Revocation is terminal — a revoked
// key cannot be un-revoked or rotated. The load effect resolves the record
// first so a scoped caller cannot revoke another tenant's key.
func (m *Manager) Revoke(ctx context.Context, keyID id.UUID, load LoadFunc, revoke RevokeFunc) error {
	if _, err := m.settings("revoke", revoke == nil); err != nil {
		return err
	}
	if _, err := m.Get(ctx, keyID, load); err != nil {
		return err
	}
	return revoke(ctx, keyID, time.Now().UTC())
}

// Rotate mints a replacement inheriting the old key's Subject, Tenant,
// Scopes, Name, and Meta (not its expiry), and hands both writes to swap:
// persist the replacement and expire the old key after grace (zero =
// immediate cutover). Both keys verify during the grace window. swap runs
// as one transaction, so a failed rotation changes nothing. The
// replacement's plaintext comes back wrapped, as Create's does.
func (m *Manager) Rotate(ctx context.Context, keyID id.UUID, grace time.Duration, load LoadFunc, swap SwapFunc) (Key, redact.Secret[string], error) {
	var noPlaintext redact.Secret[string]
	cfg, err := m.settings("swap", swap == nil)
	if err != nil {
		return Key{}, noPlaintext, err
	}
	old, err := m.Get(ctx, keyID, load)
	if err != nil {
		return Key{}, noPlaintext, err
	}
	now := time.Now().UTC()
	if !old.RevokedAt.IsZero() {
		return Key{}, noPlaintext, ErrKeyRevoked
	}
	if !old.ExpiresAt.IsZero() && !old.ExpiresAt.After(now) {
		return Key{}, noPlaintext, ErrKeyExpired
	}
	replacement, plaintext := mint(cfg.prefix, CreateParams{
		Name:    old.Name,
		Subject: old.Subject,
		Scopes:  old.Scopes,
		Meta:    old.Meta,
	}, old.Tenant)
	if err := swap(ctx, old.ID, now.Add(grace), replacement); err != nil {
		return Key{}, noPlaintext, fmt.Errorf("apikey: rotate: %w", err)
	}
	return replacement, redact.New(plaintext), nil
}
