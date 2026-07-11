package lock

import (
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

type config struct {
	owner   string
	ttl     time.Duration
	refresh time.Duration
}

// Option configures a Lock.
type Option func(*config)

// WithTTL sets the lease duration. Default 30s.
func WithTTL(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// WithOwner sets this Lock's owner id. Default a random short id — all leases
// from one Lock share it, so the same Lock can re-acquire its own key.
func WithOwner(owner string) Option {
	return func(c *config) {
		if owner != "" {
			c.owner = owner
		}
	}
}

// WithRefreshInterval sets how often a held lease is refreshed. Default TTL/3.
func WithRefreshInterval(d time.Duration) Option {
	return func(c *config) {
		if d > 0 {
			c.refresh = d
		}
	}
}

func defaultConfig() config {
	return config{owner: id.NewShort().String(), ttl: 30 * time.Second}
}
