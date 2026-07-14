package access

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
)

// RequirePermission gates a route on a static action (the REST common case:
// one route = one action). The Subject comes from guard identity; the Resource
// from the WithResource resolver (default: a type-level check). It answers 403
// on deny/abstain/missing-subject/decider-error and never 401.
func RequirePermission(d Decider, action Action, opts ...Option) middleware.Middleware {
	cfg := newConfig(opts...)
	resolve := func(r *http.Request) (Action, Resource) {
		if cfg.resource != nil {
			return action, cfg.resource(r)
		}
		return action, Resource{}
	}
	return gate(d, cfg, resolve)
}

// Require is the escape hatch for a dynamic action: resolve returns both the
// action and the resource from the request.
func Require(d Decider, resolve func(r *http.Request) (Action, Resource), opts ...Option) middleware.Middleware {
	return gate(d, newConfig(opts...), resolve)
}

func gate(d Decider, cfg config, resolve func(r *http.Request) (Action, Resource)) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if cfg.explain {
				ctx = ExplainContext(ctx)
			}
			sub, ok := cfg.subject(r)
			if !ok {
				r = r.WithContext(withDecision(ctx, Decision{Effect: Deny, Decider: "access", Reason: "no subject"}))
				cfg.reject(w, r, nil)
				return
			}
			action, res := resolve(r)
			dec, err := Authorize(ctx, d, sub, action, res)
			r = r.WithContext(withDecision(ctx, dec))
			if dec.Effect != Allow {
				cfg.reject(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
