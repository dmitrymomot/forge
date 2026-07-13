package apikey

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/crypto/consttime"
)

var _ guard.Verifier = (*Manager)(nil)

// Verify implements guard.Verifier: it resolves a plaintext credential to
// the identity of the key's Subject. Malformed credentials (wrong prefix,
// length, or checksum) are rejected before any store access. Under
// guard.New every error collapses to an opaque 401; the sentinels serve
// metrics and direct callers.
func (m *Manager) Verify(ctx context.Context, credential string) (guard.Identity, error) {
	if !validKey(m.cfg.prefix, credential) {
		return guard.Identity{}, ErrMalformedKey
	}
	h := hashKey(credential)
	k, err := m.store.GetByHash(ctx, h)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return guard.Identity{}, ErrKeyNotFound
		}
		return guard.Identity{}, fmt.Errorf("apikey: verify: %w", err)
	}
	// Defense-in-depth: a buggy store returning the wrong record (or a
	// corrupt subject-less row) must not authenticate.
	if !consttime.StringEqual(k.Hash, h) || k.Subject == "" {
		return guard.Identity{}, ErrKeyNotFound
	}
	if !k.RevokedAt.IsZero() {
		return guard.Identity{}, ErrKeyRevoked
	}
	now := time.Now().UTC()
	if !k.ExpiresAt.IsZero() && !k.ExpiresAt.After(now) {
		return guard.Identity{}, ErrKeyExpired
	}
	if m.cfg.touchInterval >= 0 && (k.LastUsedAt.IsZero() || now.Sub(k.LastUsedAt) >= m.cfg.touchInterval) {
		// Best-effort by design: a failed touch must not fail authentication.
		_ = m.store.Touch(ctx, k.ID, now)
	}
	meta := maps.Clone(k.Meta)
	if meta == nil {
		meta = make(map[string]string, 2)
	}
	meta["key_id"] = k.ID.String()
	if k.Name != "" {
		meta["key_name"] = k.Name
	}
	return guard.Identity{
		Subject: k.Subject,
		Tenant:  k.Tenant,
		Scopes:  slices.Clone(k.Scopes),
		Method:  guard.MethodAPIKey,
		Meta:    meta,
	}, nil
}
