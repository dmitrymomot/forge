package access

import (
	"context"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

var decisionKey = ctxkey.New[Decision]("access")

// DecisionFrom returns the Decision the middleware or Model recorded for this
// request, for auditlog and downstream handlers (and custom responders on the
// deny path). ok is false when no decision was stashed.
func DecisionFrom(ctx context.Context) (Decision, bool) {
	return decisionKey.From(ctx)
}

func withDecision(ctx context.Context, d Decision) context.Context {
	return decisionKey.With(ctx, d)
}
