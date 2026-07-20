package webhook

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// Secrets yields the signing secrets accepted for a request — several during
// rotation, resolved per request in multi-tenant apps (tenant from host or
// path). An error or an empty list rejects the request: fail closed.
type Secrets func(r *http.Request) ([][]byte, error)

// StaticSecrets adapts fixed secrets for the single-tenant case. The secrets
// are cloned at construction; empty ones are dropped, and dropping them all
// yields a middleware that rejects every request.
func StaticSecrets(secrets ...[]byte) Secrets {
	cp := make([][]byte, 0, len(secrets))
	for _, s := range secrets {
		if len(s) > 0 {
			cp = append(cp, slices.Clone(s))
		}
	}
	return func(*http.Request) ([][]byte, error) { return cp, nil }
}

type verifyConfig struct {
	responder func(http.ResponseWriter, *http.Request, error)
	tolerance time.Duration
	maxBody   int64
}

// VerifyOption configures the Verify middleware.
type VerifyOption func(*verifyConfig)

// WithTolerance sets the timestamp tolerance window (default 5m). Zero
// disables the timestamp check; negative values are ignored.
func WithTolerance(d time.Duration) VerifyOption {
	return func(c *verifyConfig) {
		if d >= 0 {
			c.tolerance = d
		}
	}
}

// WithMaxBody caps the inbound body size in bytes (default 1 MiB). Larger
// requests are rejected with 413 before verification. Non-positive values
// are ignored.
func WithMaxBody(n int64) VerifyOption {
	return func(c *verifyConfig) {
		if n > 0 {
			c.maxBody = n
		}
	}
}

// WithResponder replaces the rejection writer (default: problem+json with the
// status from the sentinel — 401, 413 for ErrBodyTooLarge, 400 for
// ErrUnreadableBody). Unlike the default, a custom responder receives the
// full wrapped error chain (match with errors.Is) — keep lookup detail out
// of the response body yourself. Nil is ignored.
func WithResponder(fn func(w http.ResponseWriter, r *http.Request, err error)) VerifyOption {
	return func(c *verifyConfig) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// Verify returns middleware that authenticates every request's body against
// its signature headers through scheme, fail closed. The body is read capped
// (WithMaxBody) and restored, so downstream handlers read it normally.
// Verification passes when any secret from secrets authenticates the payload
// — hand out several during rotation. Rejections answer problem+json (401 by
// default) built from a bare sentinel, so scheme and lookup internals never
// reach the caller. Panics on nil scheme or secrets — wiring bugs.
func Verify(scheme Scheme, secrets Secrets, opts ...VerifyOption) middleware.Middleware {
	if scheme == nil {
		panic("webhook: Verify with nil scheme")
	}
	if secrets == nil {
		panic("webhook: Verify with nil secrets")
	}
	jsonResponder := problem.JSON(problem.WithStatusOf(statusOf))
	cfg := verifyConfig{
		tolerance: 5 * time.Minute,
		maxBody:   1 << 20,
		// The default responder collapses the chain to its bare sentinel
		// before handing off to problem.JSON, which otherwise puts
		// err.Error() straight into the response body for any 4xx status.
		responder: func(w http.ResponseWriter, r *http.Request, err error) {
			jsonResponder(w, r, collapse(err))
		},
	}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cfg.maxBody))
			if err != nil {
				if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
					cfg.responder(w, r, fmt.Errorf("%w: cap %d bytes", ErrBodyTooLarge, cfg.maxBody))
				} else {
					cfg.responder(w, r, fmt.Errorf("%w: %w", ErrUnreadableBody, err))
				}
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			keys, err := secrets(r)
			if err != nil {
				cfg.responder(w, r, fmt.Errorf("%w: secret lookup: %w", ErrInvalidSignature, err))
				return
			}
			now := time.Now()
			verr := fmt.Errorf("%w: no secrets configured", ErrInvalidSignature)
			for _, key := range keys {
				err := scheme.Verify(key, body, r.Header, now, cfg.tolerance)
				if err == nil {
					next.ServeHTTP(w, r)
					return
				}
				verr = err
			}
			cfg.responder(w, r, verr)
		})
	}
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, ErrBodyTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrUnreadableBody):
		return http.StatusBadRequest
	default:
		return http.StatusUnauthorized
	}
}

// collapse strips a rejection down to its bare sentinel so no wrapped detail
// (lookup errors, header names, caps) reaches the client.
func collapse(err error) error {
	for _, sentinel := range []error{ErrBodyTooLarge, ErrUnreadableBody, ErrMissingSignature, ErrInvalidTimestamp} {
		if errors.Is(err, sentinel) {
			return sentinel
		}
	}
	return ErrInvalidSignature
}
