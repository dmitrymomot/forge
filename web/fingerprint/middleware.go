package fingerprint

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/web/middleware"
)

// Result is the per-request fingerprint output cached by Middleware.
type Result struct {
	Signals     []Signal
	Fingerprint Fingerprint
}

var resultKey = ctxkey.New[Result]("fingerprint")

// Middleware computes the fingerprint and signals once per request and caches
// the Result in context for FromContext and LogExtractor. A collector error is
// logged at Debug; fingerprinting never fails the request.
func (fp *Fingerprinter) Middleware() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f, err := fp.FromRequest(r)
			if err != nil && fp.logger.Enabled(r.Context(), slog.LevelDebug) {
				fp.logger.DebugContext(r.Context(), "fingerprint: collector error", slog.Any("error", err))
			}
			res := Result{Fingerprint: f, Signals: fp.Signals(r, f)}
			next.ServeHTTP(w, r.WithContext(resultKey.With(r.Context(), res)))
		})
	}
}

// FromContext returns the Result cached by Middleware. ok reports whether the
// middleware ran.
func FromContext(ctx context.Context) (Result, bool) { return resultKey.From(ctx) }

// LogExtractor adds a "fingerprint" group (hash + comma-joined names of signals
// whose Value is true) when Middleware cached a Result. Wire it with
// logger.WithContextExtractors(fingerprint.LogExtractor).
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	res, ok := resultKey.From(ctx)
	if !ok || res.Fingerprint.Hash == "" {
		return slog.Attr{}, false
	}
	attrs := []any{slog.String("hash", res.Fingerprint.Hash)}
	var flagged []string
	for _, s := range res.Signals {
		if s.Value {
			flagged = append(flagged, s.Name)
		}
	}
	if len(flagged) > 0 {
		attrs = append(attrs, slog.String("signals", strings.Join(flagged, ",")))
	}
	return slog.Group("fingerprint", attrs...), true
}
