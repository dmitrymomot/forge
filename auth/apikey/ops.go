package apikey

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Create mints a key for p and persists it through save, returning the
// stored record and the plaintext. The plaintext is shown exactly once —
// only its hash is persisted. Supply a save closure over the transaction
// that also writes whatever the application stores alongside the key.
func Create(ctx context.Context, cfg Config, p CreateParams, save SaveFunc) (Key, string, error) {
	if err := cfg.check(); err != nil {
		return Key{}, "", err
	}
	if save == nil {
		return Key{}, "", fmt.Errorf("%w: save", ErrNilEffect)
	}
	if p.Subject == "" {
		return Key{}, "", ErrSubjectRequired
	}
	tenant, err := cfg.scoped(ctx, p.Tenant)
	if err != nil {
		return Key{}, "", err
	}
	k, plaintext := mint(cfg.prefix, p, tenant)
	if err := save(ctx, k); err != nil {
		return Key{}, "", fmt.Errorf("apikey: create: %w", err)
	}
	return k, plaintext, nil
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
func Get(ctx context.Context, cfg Config, keyID id.UUID, load LoadFunc) (Key, error) {
	if err := cfg.check(); err != nil {
		return Key{}, err
	}
	if load == nil {
		return Key{}, fmt.Errorf("%w: load", ErrNilEffect)
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
func List(ctx context.Context, cfg Config, f Filter, list ListFunc) ([]Key, error) {
	if err := cfg.check(); err != nil {
		return nil, err
	}
	if list == nil {
		return nil, fmt.Errorf("%w: list", ErrNilEffect)
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
func Revoke(ctx context.Context, cfg Config, keyID id.UUID, load LoadFunc, revoke RevokeFunc) error {
	if revoke == nil {
		return fmt.Errorf("%w: revoke", ErrNilEffect)
	}
	if _, err := Get(ctx, cfg, keyID, load); err != nil {
		return err
	}
	return revoke(ctx, keyID, time.Now().UTC())
}

// Rotate mints a replacement inheriting the old key's Subject, Tenant,
// Scopes, Name, and Meta (not its expiry), and hands both writes to swap:
// persist the replacement and expire the old key after grace (zero =
// immediate cutover). Both keys verify during the grace window. swap runs
// as one transaction, so a failed rotation changes nothing.
func Rotate(ctx context.Context, cfg Config, keyID id.UUID, grace time.Duration, load LoadFunc, swap SwapFunc) (Key, string, error) {
	if swap == nil {
		return Key{}, "", fmt.Errorf("%w: swap", ErrNilEffect)
	}
	old, err := Get(ctx, cfg, keyID, load)
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
	replacement, plaintext := mint(cfg.prefix, CreateParams{
		Name:    old.Name,
		Subject: old.Subject,
		Scopes:  old.Scopes,
		Meta:    old.Meta,
	}, old.Tenant)
	if err := swap(ctx, old.ID, now.Add(grace), replacement); err != nil {
		return Key{}, "", fmt.Errorf("apikey: rotate: %w", err)
	}
	return replacement, plaintext, nil
}
