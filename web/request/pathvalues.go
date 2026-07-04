package request

import (
	"context"
	"maps"
	"net/http"
)

type pathValuesKeyType struct{}

var pathValuesKey = pathValuesKeyType{}

// pathValuesCtx carries fallback path values in a single heap allocation
// (plus the map), instead of the two context.WithValue would cost. Value
// returns the same map every call, so reads never allocate.
type pathValuesCtx struct {
	context.Context
	vals map[string]string
}

func (c *pathValuesCtx) Value(key any) any {
	if key == pathValuesKey {
		return c.vals
	}
	return c.Context.Value(key)
}

// WithPathValues returns a context carrying vals as fallback path values for
// Path, PathFunc, and HasPath. Values merge over any previously stored ones
// (later wins); the input map is copied, so later caller mutations have no
// effect. Routers that dispatch across ServeMux boundaries (subroute) use
// this to keep path params readable in nested handlers.
func WithPathValues(ctx context.Context, vals map[string]string) context.Context {
	if len(vals) == 0 {
		return ctx
	}
	var outer map[string]string
	if v, ok := ctx.Value(pathValuesKey).(map[string]string); ok {
		outer = v
	}
	merged := make(map[string]string, len(outer)+len(vals))
	maps.Copy(merged, outer)
	maps.Copy(merged, vals)
	return &pathValuesCtx{Context: ctx, vals: merged}
}

// pathValue resolves a path wildcard: the current mux's own match first,
// then WithPathValues fallback, then "". Treating an empty r.PathValue
// result as absent is safe: a ServeMux single-segment wildcard never
// matches an empty segment, so "" can only mean the key was not matched.
func pathValue(r *http.Request, key string) string {
	if v := r.PathValue(key); v != "" {
		return v
	}
	if vals, ok := r.Context().Value(pathValuesKey).(map[string]string); ok {
		return vals[key]
	}
	return ""
}
