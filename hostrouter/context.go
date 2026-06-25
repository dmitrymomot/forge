package hostrouter

import "context"

// Match describes how a request was routed. The Router injects it into the request
// context (unless WithoutMatchContext is set); read it with FromContext or the
// Subdomain/Pattern/Host accessors.
type Match struct {
	Host      string // normalized host that matched, e.g. "foo.example.com"
	Pattern   string // registered pattern, e.g. "*.example.com" or "api.example.com"
	Subdomain string // captured wildcard label ("foo"); "" for exact matches
}

type ctxKey struct{}

var matchKey = ctxKey{}

// matchCtx carries a Match in a single heap allocation, instead of the two that
// context.WithValue would cost (a *valueCtx node plus boxing the value). Value
// returns the same pointer every call, so reads never allocate.
type matchCtx struct {
	context.Context
	m Match
}

func (c *matchCtx) Value(key any) any {
	if key == matchKey {
		return &c.m
	}
	return c.Context.Value(key)
}

// FromContext returns the Match injected by the Router. ok is false when there was
// no match (the fallback handler) or injection was disabled with WithoutMatchContext.
// The returned Match is a copy; callers cannot mutate the Router's value.
func FromContext(ctx context.Context) (Match, bool) {
	if m, ok := ctx.Value(matchKey).(*Match); ok {
		return *m, true
	}
	return Match{}, false
}

// Subdomain returns the captured wildcard label, or "" if absent.
func Subdomain(ctx context.Context) string { m, _ := FromContext(ctx); return m.Subdomain }

// Pattern returns the matched registered pattern, or "" if absent.
func Pattern(ctx context.Context) string { m, _ := FromContext(ctx); return m.Pattern }

// Host returns the normalized matched host, or "" if absent.
func Host(ctx context.Context) string { m, _ := FromContext(ctx); return m.Host }
