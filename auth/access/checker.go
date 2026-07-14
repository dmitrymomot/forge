package access

import (
	"context"
	"net/http"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/middleware"
)

// checker binds an app decider to the request's resolved Subject so view code
// can ask the SAME authorization question the route gate asks.
type checker struct {
	d   Decider
	sub Subject
	ok  bool
}

var checkerKey = ctxkey.New[checker]("access.checker")

// WithChecker resolves the Subject once per request (the same resolver
// RequirePermission uses — default guard) and binds {decider, subject} into
// the context. Mount it alongside the gate so Can/CanResource run the exact
// decider chain the routes gate on. It never denies; it only binds.
func WithChecker(d Decider, opts ...Option) middleware.Middleware {
	cfg := newConfig(opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sub, ok := cfg.subject(r)
			ctx := checkerKey.With(r.Context(), checker{d: d, sub: sub, ok: ok})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Can reports whether the bound subject is allowed to perform action on a
// type-level (zero) resource. False when no checker is bound or no subject was
// resolved. For conditional rendering (show/hide a control).
func Can(ctx context.Context, action Action) bool {
	return CanResource(ctx, action, Resource{})
}

// CanResource is Can with an explicit resource — for acl/abac view checks that
// depend on the object (owner, tenant, attributes).
func CanResource(ctx context.Context, action Action, r Resource) bool {
	c, ok := checkerKey.From(ctx)
	if !ok || !c.ok {
		return false
	}
	dec, err := Authorize(ctx, c.d, c.sub, action, r)
	return err == nil && dec.Effect == Allow
}
