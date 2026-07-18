package tenant

import (
	"context"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

var idKey = ctxkey.New[string]("tenant")

// NewContext returns a copy of ctx carrying the tenant ID. It is the
// transport-agnostic carrier: HTTP middleware stamps it automatically, queue
// handlers and cron jobs stamp it themselves before touching tenant data.
func NewContext(ctx context.Context, id string) context.Context {
	return idKey.With(ctx, id)
}

// FromContext returns the tenant ID carried by ctx. ok is false when no
// tenant was stamped or the stamped ID is empty.
func FromContext(ctx context.Context) (string, bool) {
	id, ok := idKey.From(ctx)
	return id, ok && id != ""
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
