package middlewares

import (
	"strconv"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/ratelimit"
)

type rateLimitInfoKey struct{}

type rateLimitOptions struct {
	keyFunc      ratelimit.KeyFunc
	errorHandler func(internal.Context, ratelimit.Info) error
	skipFunc     func(internal.Context) bool
}

// RateLimitOption configures the RateLimit middleware.
type RateLimitOption func(*rateLimitOptions)

// WithRateLimitKeyFunc sets a custom key extraction function.
// Default: ratelimit.KeyByIP.
func WithRateLimitKeyFunc(fn ratelimit.KeyFunc) RateLimitOption {
	return func(o *rateLimitOptions) {
		o.keyFunc = fn
	}
}

// WithRateLimitErrorHandler sets a custom handler for rate-limited requests.
// The handler receives the context and the rate limit info.
func WithRateLimitErrorHandler(fn func(internal.Context, ratelimit.Info) error) RateLimitOption {
	return func(o *rateLimitOptions) {
		o.errorHandler = fn
	}
}

// WithRateLimitSkipFunc sets a function to bypass rate limiting for specific requests.
func WithRateLimitSkipFunc(fn func(internal.Context) bool) RateLimitOption {
	return func(o *rateLimitOptions) {
		o.skipFunc = fn
	}
}

// RateLimit returns middleware that enforces rate limits using the provided limiter.
// It always sets standard rate limit headers on every response.
// On counter errors, the middleware fails open (allows the request through).
// Panics if limiter is nil.
func RateLimit(limiter *ratelimit.Limiter, opts ...RateLimitOption) internal.Middleware {
	if limiter == nil {
		panic("middlewares: RateLimit requires a non-nil limiter")
	}

	o := &rateLimitOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if o.keyFunc == nil {
		o.keyFunc = ratelimit.KeyByIP
	}
	if o.errorHandler == nil {
		o.errorHandler = func(c internal.Context, info ratelimit.Info) error {
			return internal.ErrTooManyRequests("rate limit exceeded")
		}
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			if o.skipFunc != nil && o.skipFunc(c) {
				return next(c)
			}

			key := o.keyFunc(c.Request())
			info, err := limiter.Allow(c.Context(), key)
			if err != nil {
				c.LogWarn("ratelimit: counter error, failing open", "error", err)
				return next(c)
			}

			c.Set(rateLimitInfoKey{}, &info)
			setRateLimitHeaders(c, info)

			if !info.IsAllowed() {
				retrySeconds := max(int(info.RetryAfter.Seconds()), 1)
				c.SetHeader("Retry-After", strconv.Itoa(retrySeconds))
				return o.errorHandler(c, info)
			}

			return next(c)
		}
	}
}

// setRateLimitHeaders writes standard rate limit headers.
func setRateLimitHeaders(c internal.Context, info ratelimit.Info) {
	c.SetHeader("X-RateLimit-Limit", strconv.FormatInt(info.Limit, 10))
	c.SetHeader("X-RateLimit-Remaining", strconv.FormatInt(info.Remaining, 10))
	c.SetHeader("X-RateLimit-Reset", strconv.FormatInt(info.ResetAt.Unix(), 10))
}

// GetRateLimitInfo returns the rate limit info stored in the context by the RateLimit middleware.
// Returns nil if the middleware was not applied or the request was skipped.
func GetRateLimitInfo(c internal.Context) *ratelimit.Info {
	v, ok := c.Get(rateLimitInfoKey{}).(*ratelimit.Info)
	if !ok {
		return nil
	}
	return v
}
