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

// Verify resolves a plaintext credential to the identity of the key's
// Subject. Malformed credentials (wrong prefix, length, or checksum) are
// rejected before load runs. A nil touch disables last-used tracking, as
// WithTouchInterval(-1) does. Under guard.New every error collapses to an
// opaque 401; the sentinels serve metrics and direct callers.
func Verify(ctx context.Context, cfg Config, credential string, load LoadByHashFunc, touch TouchFunc) (guard.Identity, error) {
	if err := cfg.check(); err != nil {
		return guard.Identity{}, err
	}
	if load == nil {
		return guard.Identity{}, fmt.Errorf("%w: load", ErrNilEffect)
	}
	if !validKey(cfg.prefix, credential) {
		return guard.Identity{}, ErrMalformedKey
	}
	h := hashKey(credential)
	k, err := load(ctx, h)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return guard.Identity{}, ErrKeyNotFound
		}
		return guard.Identity{}, fmt.Errorf("apikey: verify: %w", err)
	}
	if !loadedRecordAuthenticates(k, h) {
		return guard.Identity{}, ErrKeyNotFound
	}
	if !k.RevokedAt.IsZero() {
		return guard.Identity{}, ErrKeyRevoked
	}
	now := time.Now().UTC()
	if !k.ExpiresAt.IsZero() && !k.ExpiresAt.After(now) {
		return guard.Identity{}, ErrKeyExpired
	}
	if touch != nil && cfg.touchDue(k, now) {
		_ = touch(ctx, k.ID, now)
	}
	return identityOf(k), nil
}

// loadedRecordAuthenticates rejects a record a buggy or corrupt load
// returned: one whose hash does not match the credential, or one with no
// subject to authenticate as.
func loadedRecordAuthenticates(k Key, hash string) bool {
	return consttime.StringEqual(k.Hash, hash) && k.Subject != ""
}

// touchDue reports whether the record's last-used stamp is stale enough to
// rewrite. A negative interval disables tracking.
func (c Config) touchDue(k Key, now time.Time) bool {
	if c.touchInterval < 0 {
		return false
	}
	return k.LastUsedAt.IsZero() || now.Sub(k.LastUsedAt) >= c.touchInterval
}

func identityOf(k Key) guard.Identity {
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
	}
}

// NewVerifier curries cfg and the effects into the guard.Verifier seam, so
// middleware wiring needs no adapter type. It reports ErrConfig for an
// unvalidated cfg and ErrNilEffect for a nil load; a nil touch disables
// last-used tracking.
func NewVerifier(cfg Config, load LoadByHashFunc, touch TouchFunc) (guard.Verifier, error) {
	if err := cfg.check(); err != nil {
		return nil, err
	}
	if load == nil {
		return nil, fmt.Errorf("%w: load", ErrNilEffect)
	}
	return guard.VerifierFunc(func(ctx context.Context, credential string) (guard.Identity, error) {
		return Verify(ctx, cfg, credential, load, touch)
	}), nil
}
