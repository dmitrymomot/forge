package otp

import (
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
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
