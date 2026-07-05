package httpclient

import (
	"context"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

// buildBreaker returns a per-host breaker func, or nil when the breaker is off.
func buildBreaker(c config) breakerFunc {
	if !c.useBreaker {
		return nil
	}
	group := circuitbreaker.NewGroup(c.breakerOpts...)
	return func(ctx context.Context, host string, fn func(context.Context) error) error {
		return group.Do(ctx, host, fn)
	}
}
