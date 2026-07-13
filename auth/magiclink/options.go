package magiclink

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/secret"
	"github.com/dmitrymomot/forge/crypto/token"
	"github.com/dmitrymomot/forge/resilience/cache"
)

type config struct {
	clk     clock.Clock
	store   cache.Store
	box     *secret.Box
	scopeFn func(context.Context) (string, error)
	errs    []error
	ttl     time.Duration
}

// Option configures New/FromKeyset.
type Option func(*config)

func newConfig(purpose string, opts ...Option) (*config, error) {
	c := &config{clk: clock.System(), ttl: 15 * time.Minute}
	for _, o := range opts {
		o(c)
	}
	if purpose == "" {
		c.errs = append(c.errs, errors.New("magiclink: empty purpose"))
	}
	if c.ttl <= 0 {
		c.errs = append(c.errs, errors.New("magiclink: ttl must be positive"))
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return c, nil
}

// codecOptions translates the resolved config into crypto/token options.
func (c *config) codecOptions(purpose string) []token.Option {
	opts := []token.Option{
		token.WithTTL(c.ttl),
		token.WithPurpose(purpose),
		token.WithClock(c.clk),
	}
	if c.box != nil {
		opts = append(opts, token.WithEncrypt(c.box))
	}
	return opts
}

// WithTTL sets the link lifetime (default 15m). Links always expire: a
// non-positive d is a constructor error.
func WithTTL(d time.Duration) Option { return func(c *config) { c.ttl = d } }

// WithClock sets the time source (default clock.System()). A nil clock is
// rejected.
func WithClock(ck clock.Clock) Option {
	return func(c *config) {
		if ck == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil clock"))
			return
		}
		c.clk = ck
	}
}

// WithEncrypt encrypts the payload (not just signs it), hiding PII from the
// URL. A nil box is rejected.
func WithEncrypt(box *secret.Box) Option {
	return func(c *config) {
		if box == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil box"))
			return
		}
		c.box = box
	}
}

// WithStore enables single-use redemption: Redeem atomically claims each link
// and returns ErrUsed on replay. A nil store is rejected. The bundled LRU
// memory store can evict live keys; production single-use needs cache/redis
// or another durable Store.
func WithStore(s cache.Store) Option {
	return func(c *config) {
		if s == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil store"))
			return
		}
		c.store = s
	}
}

// WithScope binds links to a tenant scope resolved from ctx (forge-wide
// multi-tenancy hook). Issue stamps the resolved scope into the token;
// Peek/Redeem recompute it and fail closed on mismatch. An empty resolved
// scope means a global link, valid in any tenant context; a hook that wants
// to forbid global issuance returns an error when ctx lacks a tenant. A nil
// hook is rejected.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, errors.New("magiclink: nil scope hook"))
			return
		}
		c.scopeFn = fn
	}
}
