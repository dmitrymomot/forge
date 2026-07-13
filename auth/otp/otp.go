package otp

import (
	"context"
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
