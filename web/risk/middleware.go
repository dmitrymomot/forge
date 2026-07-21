package risk

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/web/middleware"
)

type middlewareConfig struct {
	log    *slog.Logger
	reject func(w http.ResponseWriter, r *http.Request, err error)
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithRejectHandler replaces the default plain 403 written on any Check
// failure — gate trip or scorer error alike. Use it to divert to a decoy
// destination or render a problem response. err is the Check error; a gate
// trip matches errors.Is(err, ErrFraud).
func WithRejectHandler(h func(w http.ResponseWriter, r *http.Request, err error)) MiddlewareOption {
	return func(c *middlewareConfig) { c.reject = h }
}

// WithLogger sets the logger for rejection and scorer-failure records.
// Defaults to logger.NewNope; nil is ignored.
func WithLogger(l *slog.Logger) MiddlewareOption {
	return func(c *middlewareConfig) {
		if l != nil {
			c.log = l
		}
	}
}

// Middleware gates requests through e. buildInput assembles the scorer input
// from the request — the raw *http.Request is richer than any digested visit
// context, so scorers here can read headers, fingerprints, and client IPs
// directly. Check nil proceeds to next (an OnFraud handler returning nil
// lands here — shadow mode is invisible to the middleware). Any Check error
// — fraud or scorer infrastructure failure — rejects fail-closed: a gate
// trip logs at Warn with score attribution, an infrastructure error at
// Error, then the reject handler writes the response (default: plain 403,
// no error detail in the body). Panics on a nil engine or buildInput.
func Middleware[T any](e *Engine[T], buildInput func(*http.Request) T, opts ...MiddlewareOption) middleware.Middleware {
	if e == nil {
		panic("risk: Middleware requires a non-nil Engine")
	}
	if buildInput == nil {
		panic("risk: Middleware requires a non-nil buildInput")
	}
	cfg := middlewareConfig{log: logger.NewNope(), reject: defaultReject}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := e.Check(r.Context(), buildInput(r))
			if err == nil {
				next.ServeHTTP(w, r)
				return
			}
			if fraud, ok := errors.AsType[*FraudError](err); ok {
				cfg.log.WarnContext(r.Context(), "request rejected as fraud",
					slog.Float64("score", fraud.Score.Value),
					slog.Float64("peak", fraud.Score.Peak),
					slog.Int("peak_scorer", fraud.Score.PeakIdx),
					slog.String("path", r.URL.Path))
			} else {
				cfg.log.ErrorContext(r.Context(), "risk check failed",
					slog.Any("error", err),
					slog.String("path", r.URL.Path))
			}
			cfg.reject(w, r, err)
		})
	}
}

func defaultReject(w http.ResponseWriter, _ *http.Request, _ error) {
	http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
}
