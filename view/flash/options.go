package flash

import (
	"fmt"
	"time"
)

// config holds the resolved settings shared by both stores.
type config struct {
	name     string
	errs     []error
	lifetime time.Duration
}

// Option configures a store. Invalid values accumulate and are returned by the
// constructor.
type Option func(*config)

// WithCookieName renames the cookie the store writes (default DefaultCookieName). An
// empty name is rejected.
func WithCookieName(name string) Option {
	return func(c *config) {
		if name == "" {
			c.errs = append(c.errs, fmt.Errorf("%w: WithCookieName received an empty name", ErrInvalidConfig))
			return
		}
		c.name = name
	}
}

// WithLifetime bounds how long an unread message survives (default DefaultLifetime).
// It is both the cookie's Max-Age and, for CacheStore, the stored entry's TTL. A
// non-positive duration is rejected.
func WithLifetime(d time.Duration) Option {
	return func(c *config) {
		if d <= 0 {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLifetime requires a positive duration, got %s", ErrInvalidConfig, d))
			return
		}
		c.lifetime = d
	}
}

func (c config) validate() error {
	if len(c.errs) == 0 {
		return nil
	}
	return joinErrs(c.errs)
}
