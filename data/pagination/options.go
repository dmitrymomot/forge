package pagination

import (
	"errors"

	"github.com/dmitrymomot/forge/crypto/sign"
)

// config holds resolved codec settings before construction.
type config struct {
	signer *sign.Signer
	errs   []error
}

// Option configures NewCodec. Invalid values accumulate and are returned.
type Option func(*config)

func newConfig(opts ...Option) (*config, error) {
	c := &config{}
	for _, o := range opts {
		o(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return c, nil
}

// WithSigner signs cursors with the given HMAC signer, making them
// tamper-evident: Decode returns ErrBadCursor for a forged or altered cursor.
// A keyset-backed signer (sign.FromKeyset) verifies cursors across key
// rotation. A nil signer is rejected with ErrNilSigner.
func WithSigner(s *sign.Signer) Option {
	return func(c *config) {
		if s == nil {
			c.errs = append(c.errs, ErrNilSigner)
			return
		}
		c.signer = s
	}
}
