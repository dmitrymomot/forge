package invite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/consttime"
)

// Manager issues, manages, and accepts tenant invitations over a Store.
// Safe for concurrent use.
type Manager struct {
	store Store
	cfg   config
}

// New builds a Manager. It panics on a nil store or a non-positive TTL —
// wiring bugs caught at startup, like apikey.New's nil-store panic.
func New(store Store, opts ...Option) *Manager {
	if store == nil {
		panic("invite: nil store")
	}
	cfg := config{ttl: 7 * 24 * time.Hour}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.ttl <= 0 {
		panic(fmt.Sprintf("invite: non-positive TTL %s", cfg.ttl))
	}
	return &Manager{store: store, cfg: cfg}
}

// Create mints an invite for p, returning the stored record and the
// plaintext token. The plaintext is shown exactly once — only its hash is
// persisted; sending it (comms/email) is the consumer's job.
func (m *Manager) Create(ctx context.Context, p CreateParams) (Invite, string, error) {
	if p.Email == "" {
		return Invite{}, "", ErrEmailRequired
	}
	tenant, err := m.scoped(ctx, p.Tenant)
	if err != nil {
		return Invite{}, "", err
	}
	plaintext := newToken()
	now := time.Now().UTC()
	inv := Invite{
		ID:        id.NewUUID(),
		Hash:      hashToken(plaintext),
		Email:     p.Email,
		Tenant:    tenant,
		Role:      p.Role,
		CreatedAt: now,
		ExpiresAt: now.Add(m.cfg.ttl),
	}
	if err := m.store.Create(ctx, inv); err != nil {
		return Invite{}, "", fmt.Errorf("invite: create: %w", err)
	}
	return inv, plaintext, nil
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

// Get returns one invite record. With WithScope configured, other tenants'
// invites read as ErrNotFound so existence cannot be probed across tenants.
func (m *Manager) Get(ctx context.Context, inviteID id.UUID) (Invite, error) {
	tenant, err := m.scoped(ctx, "")
	if err != nil {
		return Invite{}, err
	}
	inv, err := m.store.Get(ctx, inviteID)
	if err != nil {
		return Invite{}, err
	}
	if m.cfg.scope != nil && inv.Tenant != tenant {
		return Invite{}, ErrNotFound
	}
	return inv, nil
}

// List returns invites matching f, newest first. With WithScope configured
// the filter is confined to the scoped tenant.
func (m *Manager) List(ctx context.Context, f Filter) ([]Invite, error) {
	tenant, err := m.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, err
	}
	f.Tenant = tenant
	return m.store.List(ctx, f)
}

// Revoke withdraws a pending invite so its token can never be accepted.
// Revoking an already-revoked invite is a no-op; an accepted one reports
// ErrAlreadyAccepted — revocation does not undo membership.
func (m *Manager) Revoke(ctx context.Context, inviteID id.UUID) error {
	if _, err := m.Get(ctx, inviteID); err != nil {
		return err
	}
	return m.store.Revoke(ctx, inviteID, time.Now().UTC())
}

// Resend rotates the invite's token for another delivery attempt: a fresh
// plaintext, a fresh expiry window, and the old token dead — resending a
// leaked or expired invite never revives the previous token. Accepted and
// revoked invites report ErrAlreadyAccepted / ErrRevoked. The send itself
// stays consumer-side.
func (m *Manager) Resend(ctx context.Context, inviteID id.UUID) (Invite, string, error) {
	inv, err := m.Get(ctx, inviteID)
	if err != nil {
		return Invite{}, "", err
	}
	switch {
	case !inv.AcceptedAt.IsZero():
		return Invite{}, "", ErrAlreadyAccepted
	case !inv.RevokedAt.IsZero():
		return Invite{}, "", ErrRevoked
	}
	plaintext := newToken()
	hash := hashToken(plaintext)
	expiresAt := time.Now().UTC().Add(m.cfg.ttl)
	if err := m.store.Rotate(ctx, inviteID, hash, expiresAt); err != nil {
		return Invite{}, "", fmt.Errorf("invite: resend: %w", err)
	}
	inv.Hash = hash
	inv.ExpiresAt = expiresAt
	return inv, plaintext, nil
}

// Accept redeems a plaintext token exactly once and returns the verified
// claim. Malformed tokens are rejected before any store access; the
// store's atomic accept is the single-use guard, so under a race exactly
// one caller wins and the rest see ErrAlreadyAccepted. Accept is
// deliberately unscoped — the invitee typically has no tenant context yet;
// the returned Claim carries the tenant to join.
func (m *Manager) Accept(ctx context.Context, token string) (Claim, error) {
	inv, err := m.verify(ctx, token)
	if err != nil {
		return Claim{}, err
	}
	if err := m.store.Accept(ctx, inv.ID, time.Now().UTC()); err != nil {
		if errors.Is(err, ErrNotFound) {
			// The record vanished between lookup and accept.
			return Claim{}, ErrInviteNotFound
		}
		return Claim{}, fmt.Errorf("invite: accept: %w", err)
	}
	return claim(inv), nil
}

// Peek verifies a token without consuming it — serve it on GET so email
// scanners that prefetch links cannot burn the invite, and to render the
// "join <tenant>" page before the user commits. The consuming Accept is
// authoritative.
func (m *Manager) Peek(ctx context.Context, token string) (Claim, error) {
	inv, err := m.verify(ctx, token)
	if err != nil {
		return Claim{}, err
	}
	return claim(inv), nil
}

// verify resolves a plaintext token to its still-pending invite record.
func (m *Manager) verify(ctx context.Context, token string) (Invite, error) {
	if !validToken(token) {
		return Invite{}, ErrMalformedToken
	}
	h := hashToken(token)
	inv, err := m.store.GetByHash(ctx, h)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Invite{}, ErrInviteNotFound
		}
		return Invite{}, fmt.Errorf("invite: verify: %w", err)
	}
	// Defense-in-depth: a buggy store returning the wrong record (or a
	// corrupt email-less row) must not mint a claim.
	if !consttime.StringEqual(inv.Hash, h) || inv.Email == "" {
		return Invite{}, ErrInviteNotFound
	}
	switch {
	case !inv.RevokedAt.IsZero():
		return Invite{}, ErrRevoked
	case !inv.AcceptedAt.IsZero():
		return Invite{}, ErrAlreadyAccepted
	case !inv.ExpiresAt.After(time.Now().UTC()):
		return Invite{}, ErrExpired
	}
	return inv, nil
}

func claim(inv Invite) Claim {
	return Claim{Tenant: inv.Tenant, Email: inv.Email, Role: inv.Role, ID: inv.ID}
}
