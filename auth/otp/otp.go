package otp

import (
	"context"
	"crypto/hmac"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/resilience/cache"
)

const minSecretLen = 16

// OTP issues and verifies short numeric one-time codes for a single flow
// (purpose). Create one instance per flow; see WithPurpose.
type OTP struct {
	store  cache.Store
	secret []byte
	cfg    config
}

// New returns an OTP issuer/verifier. secret is the HMAC pepper for codes
// at rest (min 16 bytes; load it from the environment, e.g. OTP_SECRET) and
// store is the shared TTL-KV seam (cache.NewMemoryStore for tests/dev,
// cache/redis in production).
func New(secret []byte, store cache.Store, opts ...Option) (*OTP, error) {
	cfg := config{
		clock:       clock.System(),
		purpose:     "default",
		ttl:         10 * time.Minute,
		length:      6,
		maxAttempts: 5,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	switch {
	case len(secret) < minSecretLen:
		return nil, fmt.Errorf("%w: secret must be at least %d bytes", ErrInvalidConfig, minSecretLen)
	case store == nil:
		return nil, fmt.Errorf("%w: store is required", ErrInvalidConfig)
	case cfg.purpose == "":
		return nil, fmt.Errorf("%w: purpose must not be empty", ErrInvalidConfig)
	case cfg.ttl <= 0:
		return nil, fmt.Errorf("%w: ttl must be positive", ErrInvalidConfig)
	case cfg.length < 4 || cfg.length > 10:
		return nil, fmt.Errorf("%w: length must be in [4,10]", ErrInvalidConfig)
	case cfg.maxAttempts < 1 || cfg.maxAttempts > 255:
		return nil, fmt.Errorf("%w: max attempts must be in [1,255]", ErrInvalidConfig)
	case cfg.clock == nil:
		return nil, fmt.Errorf("%w: clock must not be nil", ErrInvalidConfig)
	}
	return &OTP{store: store, cfg: cfg, secret: secret}, nil
}

// Generate issues a fresh code for identifier and stores its keyed hash with
// the configured TTL, replacing any previous code for the same (purpose,
// scope, identifier) — calling it again is "resend". The returned plaintext
// code is for the caller to deliver over their own channel; it is never
// stored. identifier must arrive canonicalized (lowercased email, E.164
// phone) in the exact same form Verify will receive.
func (o *OTP) Generate(ctx context.Context, identifier string) (string, error) {
	scope, err := o.resolveScope(ctx)
	if err != nil {
		return "", err
	}
	code := random.DigitCode(o.cfg.length)
	rec := record{expiresAt: o.cfg.clock.Now().Add(o.cfg.ttl).Unix()}
	copy(rec.codeHash[:], digest.HMACSHA256(o.secret, []byte(code)))
	key := o.storageKey(scope, identifier)
	if err := o.store.Set(ctx, key, encodeRecord(rec), cache.WithTTL(o.cfg.ttl)); err != nil {
		return "", errors.Join(ErrStore, err)
	}
	return code, nil
}

// resolveScope runs the configured scope hook, failing closed: a hook error
// or empty scope aborts the operation so a scoped code can never land in an
// unscoped (or another tenant's) bucket.
func (o *OTP) resolveScope(ctx context.Context) (string, error) {
	if o.cfg.scope == nil {
		return "", nil
	}
	s, err := o.cfg.scope(ctx)
	if err != nil {
		return "", errors.Join(ErrScope, err)
	}
	if s == "" {
		return "", ErrScope
	}
	return s, nil
}

// storageKey derives the deterministic store key. Scope and identifier are
// length-prefixed before hashing so composite values cannot collide, and
// hashed so no PII (emails, phone numbers) appears in store keys.
func (o *OTP) storageKey(scope, identifier string) string {
	buf := make([]byte, 0, 8+len(scope)+len(identifier))
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(scope)))
	buf = append(buf, scope...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(identifier)))
	buf = append(buf, identifier...)
	return "otp:" + o.cfg.purpose + ":" + hex.EncodeToString(digest.SHA256(buf))
}

// Verify checks code against the active record for identifier. It is
// single-use: a successful verification deletes the record. A mismatch
// consumes one attempt; consuming the last attempt (or meeting an already
// consumed limit) invalidates the code. Callers should map ErrNotFound and
// ErrCodeMismatch to one user-facing message ("invalid or expired code") so
// responses reveal nothing about code existence.
func (o *OTP) Verify(ctx context.Context, identifier, code string) error {
	scope, err := o.resolveScope(ctx)
	if err != nil {
		return err
	}
	key := o.storageKey(scope, identifier)

	raw, err := o.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return ErrNotFound
		}
		return errors.Join(ErrStore, err)
	}
	rec, ok := decodeRecord(raw)
	if !ok {
		// Unknown version or corrupt payload: treat as revoked. Best-effort
		// cleanup — the entry also dies by store TTL.
		_ = o.store.Delete(ctx, key)
		return ErrNotFound
	}
	now := o.cfg.clock.Now()
	remaining := time.Unix(rec.expiresAt, 0).Sub(now)
	if remaining <= 0 {
		// Defense-in-depth for stores that ignore TTL; also closes the
		// cache seam's TTL<=0-means-eternal edge on the rewrite below.
		_ = o.store.Delete(ctx, key)
		return ErrNotFound
	}
	if int(rec.attempts) >= o.cfg.maxAttempts {
		// Only reachable when a concurrent mismatch raced the rewrite;
		// deletion already happened or happens here.
		_ = o.store.Delete(ctx, key)
		return ErrTooManyAttempts
	}
	if !hmac.Equal(digest.HMACSHA256(o.secret, []byte(code)), rec.codeHash[:]) {
		rec.attempts++
		if int(rec.attempts) >= o.cfg.maxAttempts {
			if err := o.store.Delete(ctx, key); err != nil {
				return errors.Join(ErrStore, err)
			}
			return ErrTooManyAttempts
		}
		if err := o.store.Set(ctx, key, encodeRecord(rec), cache.WithTTL(remaining)); err != nil {
			return errors.Join(ErrStore, err)
		}
		return ErrCodeMismatch
	}
	// Single-use: the delete must succeed before success is reported, or a
	// live code would outlive a "verified" response.
	if err := o.store.Delete(ctx, key); err != nil {
		return errors.Join(ErrStore, err)
	}
	return nil
}

// Revoke deletes any outstanding code for identifier — cancel a pending
// flow, or invalidate after an account-state change. Revoking when nothing
// is outstanding is a no-op.
func (o *OTP) Revoke(ctx context.Context, identifier string) error {
	scope, err := o.resolveScope(ctx)
	if err != nil {
		return err
	}
	if err := o.store.Delete(ctx, o.storageKey(scope, identifier)); err != nil {
		return errors.Join(ErrStore, err)
	}
	return nil
}
