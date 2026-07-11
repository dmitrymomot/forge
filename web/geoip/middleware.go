package geoip

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/web/middleware"
)

var locKey = ctxkey.New[Location]("geoip")

// Middleware resolves the client Location once per request via src and caches
// it in the request context for Get, From, and LogExtractor. A miss or a src
// error caches an empty Location (From still reports the middleware ran); an
// error is logged at Debug. Geo enrichment is best-effort and never fails the
// request.
func Middleware(src Source, opts ...Option) middleware.Middleware {
	c := newConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			loc, err := src.Lookup(r)
			if err != nil {
				loc = Location{}
				if c.logger.Enabled(r.Context(), slog.LevelDebug) {
					c.logger.DebugContext(r.Context(), "geoip: source lookup failed", slog.Any("error", err))
				}
			}
			next.ServeHTTP(w, r.WithContext(locKey.With(r.Context(), loc)))
		})
	}
}

// From returns the cached Location. ok reports whether Middleware ran — true
// even when the Location is empty (resolution ran but produced nothing).
func From(ctx context.Context) (Location, bool) { return locKey.From(ctx) }

// Get returns the cached Location, or the zero Location when Middleware did not
// run (geoip has no zero-config source to fall back to — install Middleware).
func Get(r *http.Request) Location {
	loc, _ := From(r.Context())
	return loc
}

// LogExtractor adds a "geo" group attribute (only the populated fields) when
// Middleware cached a non-empty Location. Wire it with
// logger.WithContextExtractors(geoip.LogExtractor).
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	loc, ok := locKey.From(ctx)
	if !ok || loc.Empty() {
		return slog.Attr{}, false
	}
	attrs := make([]any, 0, 6)
	if loc.CountryCode != "" {
		attrs = append(attrs, slog.String("country", loc.CountryCode))
	}
	if loc.RegionCode != "" {
		attrs = append(attrs, slog.String("region", loc.RegionCode))
	}
	if loc.City != "" {
		attrs = append(attrs, slog.String("city", loc.City))
	}
	if loc.TimeZone != "" {
		attrs = append(attrs, slog.String("timezone", loc.TimeZone))
	}
	if loc.ASN != 0 {
		attrs = append(attrs, slog.Uint64("asn", uint64(loc.ASN)))
	}
	if loc.ASNOrg != "" {
		attrs = append(attrs, slog.String("asn_org", loc.ASNOrg))
	}
	return slog.Group("geo", attrs...), true
}
