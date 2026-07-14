package access

import (
	"context"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

var decisionKey = ctxkey.New[Decision]("access")

// DecisionFrom returns the Decision the middleware or Model recorded for this
// request, for auditlog and downstream handlers (and custom responders on the
// deny path). ok is false when no decision was stashed.
//
// Security: Decision.Reason (and Trace reasons) may carry internal detail — a
// decider's raw error text lands there on the fail-closed path. It is safe for
// server-side auditlog, but a custom WithResponder/WithLoadError must not echo
// it verbatim to clients. The built-in responders never do (they send generic
// errDenied/errNotFound sentinels).
func DecisionFrom(ctx context.Context) (Decision, bool) {
	return decisionKey.From(ctx)
}

func withDecision(ctx context.Context, d Decision) context.Context {
	return decisionKey.With(ctx, d)
}
