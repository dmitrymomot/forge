package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/resilience/retry"
)

// New returns a resilient *http.Client: a RoundTripper stack that decorates the
// request (static/context headers, hooks), retries idempotent methods on
// transient failures with jittered backoff (honoring Retry-After), and bounds
// each attempt. It returns the stdlib type; problem+json surfacing is a
// companion problem.Decode(resp) call.
func New(opts ...Option) *http.Client {
	c := newConfig(opts...)
	return &http.Client{Transport: &transport{cfg: c, breaker: buildBreaker(c)}, Timeout: c.timeout}
}

type transport struct {
	breaker breakerFunc // nil unless WithBreakerGroup is set
	cfg     config
}

// breakerFunc runs fn under a per-host breaker; nil means no breaker.
type breakerFunc func(ctx context.Context, host string, fn func(context.Context) error) error

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.decorate(req)

	var resp *http.Response
	attempt := func(ctx context.Context) error {
		if resp != nil { // a prior attempt's response is being superseded — release its connection
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			resp = nil
		}
		r := req.Clone(ctx)
		if req.GetBody != nil { // replay the body on retried methods (PUT/DELETE with a body)
			b, gErr := req.GetBody()
			if gErr != nil {
				return retry.Permanent(gErr)
			}
			r.Body = b
		}
		call := func(ctx context.Context) error {
			cctx := ctx
			if t.cfg.perAttempt > 0 {
				var cancel context.CancelFunc
				cctx, cancel = context.WithTimeout(ctx, t.cfg.perAttempt)
				defer cancel()
			}
			out, err := t.cfg.base.RoundTrip(r.WithContext(cctx))
			if err != nil {
				return err
			}
			resp = out
			for _, fn := range t.cfg.after {
				fn(r, out)
			}
			if retryableStatus(out.StatusCode) {
				return statusError{code: out.StatusCode, retryAfter: parseRetryAfter(out)}
			}
			return nil
		}
		if t.breaker != nil {
			return t.breaker(ctx, req.URL.Host, call)
		}
		return call(ctx)
	}

	ctx := req.Context()
	// The outer request context being canceled or hitting its overall deadline is
	// terminal — never retry past it. Everything else is retryable: a retryable
	// HTTP status (statusError), a network error, or a per-attempt timeout from
	// WithPerAttemptTimeout — the latter surfaces as context.DeadlineExceeded but
	// the outer ctx is still live (checked below), so the next attempt should run.
	// The error alone can't distinguish an outer-ctx timeout from a per-attempt
	// sub-context timeout (both wrap context.DeadlineExceeded), so this closure
	// checks the outer ctx directly instead of inspecting err.
	retryIf := func(err error) bool { return ctx.Err() == nil }

	var err error
	if t.cfg.retryMethods[req.Method] {
		err = retry.Do(ctx, attempt, append([]retry.Option{retry.WithRetryIf(retryIf)}, t.cfg.retryOpts...)...)
	} else {
		err = attempt(ctx)
	}

	// A bad-status "error" is internal to drive retry; return the response, not an error.
	if _, ok := errors.AsType[statusError](err); ok {
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (t *transport) decorate(req *http.Request) {
	for k, vs := range t.cfg.headers {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	if t.cfg.userAgent != "" {
		req.Header.Set("User-Agent", t.cfg.userAgent)
	}
	for _, fn := range t.cfg.ctxHeaders {
		for k, vs := range fn(req.Context()) {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
	}
	for _, fn := range t.cfg.before {
		fn(req)
	}
}

// statusError carries a retryable HTTP status and any Retry-After hint, so
// retry (which honors retry.RetryAfterError) can raise the delay floor.
type statusError struct {
	code       int
	retryAfter time.Duration
}

func (e statusError) Error() string             { return fmt.Sprintf("httpclient: server returned %d", e.code) }
func (e statusError) RetryAfter() time.Duration { return e.retryAfter }

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

func parseRetryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}
