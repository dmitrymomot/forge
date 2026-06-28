package sign

import (
	"crypto/sha256"
	"fmt"
	"hash"
)

// config holds resolved signer settings before construction.
type config struct {
	hash func() hash.Hash
	errs []error
}

// Option configures New/FromKeyset. Invalid values accumulate and are returned.
type Option func(*config)

func newConfig(opts ...Option) *config {
	c := &config{hash: sha256.New}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithHash sets the HMAC hash constructor (default sha256.New). A nil value is rejected.
func WithHash(h func() hash.Hash) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: nil hash", ErrInvalidKey))
			return
		}
		c.hash = h
	}
}
