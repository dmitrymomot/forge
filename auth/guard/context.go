package guard

import (
	"context"
	"log/slog"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/logger"
)

var identityKey = ctxkey.New[Identity]("guard")

// From returns the Identity stored by the guard middleware, if any.
func From(ctx context.Context) (Identity, bool) {
	return identityKey.From(ctx)
}

// MustFrom returns the Identity or panics if absent — for handlers mounted
// behind the guard, where a missing Identity is a wiring bug.
func MustFrom(ctx context.Context) Identity {
	return identityKey.MustFrom(ctx)
}

// LogExtractor adds an "auth" group with the subject (and tenant when set)
// for requests that carry an Identity.
var LogExtractor logger.ContextExtractor = func(ctx context.Context) (slog.Attr, bool) {
	id, ok := identityKey.From(ctx)
	if !ok {
		return slog.Attr{}, false
	}
	attrs := []any{slog.String("subject", id.Subject)}
	if id.Tenant != "" {
		attrs = append(attrs, slog.String("tenant", id.Tenant))
	}
	return slog.Group("auth", attrs...), true
}
