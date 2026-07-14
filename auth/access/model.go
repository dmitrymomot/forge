package access

import "net/http"

// Model binds a domain type T to the authorization seam: Load fetches the
// addressed object (consumer code — access never queries) and Describe maps it
// to a Resource. Optional sugar over the core seam.
type Model[T any] struct {
	Load     func(r *http.Request) (T, error)
	Describe func(obj T) Resource
}

// NewModel builds a Model with type inference (T comes from load's return
// type). It panics if either func is nil — a wiring bug caught at startup.
func NewModel[T any](load func(r *http.Request) (T, error), describe func(obj T) Resource) Model[T] {
	if load == nil || describe == nil {
		panic("access: NewModel requires non-nil load and describe")
	}
	return Model[T]{Load: load, Describe: describe}
}

// Handle returns an http.Handler that resolves the Subject (else 403, without
// loading), calls Load (error → 404 with a generic body that cloaks both
// resource existence and the underlying error text; see WithLoadError to opt
// into the raw error), Describes the object, authorizes the action (deny →
// 403; decider error → 403 + logged), stashes the Decision, then calls fn
// with the already-loaded object. Describe supplies the Resource, so passing
// WithResource here panics — a wiring bug caught at startup (Handle runs at
// route-registration time) rather than a silent no-op.
func (m Model[T]) Handle(d Decider, action Action, fn func(w http.ResponseWriter, r *http.Request, obj T), opts ...Option) http.Handler {
	cfg := newConfig(opts...)
	if cfg.resource != nil {
		panic("access: WithResource has no effect on Model.Handle — Describe supplies the Resource")
	}
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
		obj, err := m.Load(r)
		if err != nil {
			cfg.loadError(w, r, err)
			return
		}
		dec, aerr := Authorize(ctx, d, sub, action, m.Describe(obj))
		r = r.WithContext(withDecision(ctx, dec))
		if dec.Effect != Allow {
			cfg.reject(w, r, aerr)
			return
		}
		fn(w, r, obj)
	})
}
