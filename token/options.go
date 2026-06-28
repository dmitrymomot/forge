package token

import (
	"errors"
	"time"

	"github.com/dmitrymomot/forge/clock"
	"github.com/dmitrymomot/forge/secret"
)

type config struct {
	clk     clock.Clock
	box     *secret.Box
	purpose string
	errs    []error
	ttl     time.Duration
}

// Option configures New/FromKeyset.
type Option func(*config)

func newConfig(opts ...Option) (*config, error) {
	c := &config{clk: clock.System()}
	for _, o := range opts {
		o(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return c, nil
}

// WithTTL sets the token lifetime. A zero TTL (default) means the token never expires.
func WithTTL(d time.Duration) Option { return func(c *config) { c.ttl = d } }

// WithPurpose binds tokens to a named flow; Parse rejects a mismatched purpose.
func WithPurpose(p string) Option { return func(c *config) { c.purpose = p } }

// WithEncrypt encrypts the payload (not just signs it) using the given secret.Box.
func WithEncrypt(box *secret.Box) Option { return func(c *config) { c.box = box } }

// WithClock sets the time source (default clock.System()). A nil clock is rejected.
func WithClock(ck clock.Clock) Option {
	return func(c *config) {
		if ck == nil {
			c.errs = append(c.errs, errors.New("token: nil clock"))
			return
		}
		c.clk = ck
	}
}
