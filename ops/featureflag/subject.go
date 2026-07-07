package featureflag

import (
	"context"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

var subjectKey = ctxkey.New[string]("featureflag.subject")

// WithSubject attaches the evaluation subject ID (user/tenant/customer ID)
// to the context. Auth middleware calls this once per request. IDs should be
// globally unique (id.Prefix style) so rollout buckets don't correlate
// across tenants.
func WithSubject(ctx context.Context, id string) context.Context {
	return subjectKey.With(ctx, id)
}

// SubjectFromContext returns the subject ID set by WithSubject.
func SubjectFromContext(ctx context.Context) (string, bool) {
	return subjectKey.From(ctx)
}
