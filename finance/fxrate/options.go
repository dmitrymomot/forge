package fxrate

import (
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	clk    clock.Clock
	quotes []string
	ttl    time.Duration
}

// Option configures a Converter.
type Option func(*config)

// WithTTL sets how long a fetched snapshot serves conversions before the next
// call triggers a refresh. Default 1h. The TTL paces refetching; the
// provider's publication time stays visible as Snapshot.AsOf.
func WithTTL(d time.Duration) Option {
	return func(c *config) { c.ttl = d }
}

// WithQuotes restricts fetches to the given quote currencies instead of
// everything the source offers. Fetch fails closed if the source omits any of
// them. Codes are normalized (trimmed, uppercased).
func WithQuotes(codes ...string) Option {
	return func(c *config) {
		c.quotes = make([]string, len(codes))
		for i, code := range codes {
			c.quotes[i] = normalizeCode(code)
		}
	}
}

// WithClock overrides the time source used for TTL checks. For tests.
func WithClock(clk clock.Clock) Option {
	return func(c *config) { c.clk = clk }
}
