package tenant

import "context"

// ctxKey is the unexported context key for the tenant ID. A struct{} key is
// collision-free across packages because the type itself is unexported.
type ctxKey struct{}

// NewContext returns a copy of ctx carrying the tenant ID. It is the
// transport-agnostic carrier: HTTP middleware, queue handlers, and cron jobs
// all stamp the tenant the same way.
func NewContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the tenant ID carried by ctx. ok is false when no
// tenant was stamped or the stamped ID is empty.
func FromContext(ctx context.Context) (string, bool) {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id, id != ""
}

// Scope returns the tenant ID from ctx, failing closed with ErrNoTenant when
// absent or empty. Its signature matches the WithScope option of every other
// forge package, so it plugs in directly:
//
//	mgr := apikey.New(store, apikey.WithScope(tenant.Scope))
func Scope(ctx context.Context) (string, error) {
	id, ok := FromContext(ctx)
	if !ok {
		return "", ErrNoTenant
	}
	return id, nil
}
