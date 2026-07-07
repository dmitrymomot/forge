package featureflag

import (
	"context"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/typeconv"
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

// Evaluator is a subject-bound view of a Client for code without request
// context (jobs, CLIs). Explicit tokens substitute for the identity resolver.
//
// Because it carries no request context, request-scoped providers (e.g. a
// tenant-aware Cached/DB provider keyed on ctx) are not consulted with
// tenancy through For — use it for provider-context-free evaluation
// (static/config flags in jobs and CLIs).
type Evaluator struct {
	client *Client
	id     string
	tokens []string
}

// For binds a subject ID and optional identity tokens.
func (c *Client) For(id string, tokens ...string) Evaluator {
	return Evaluator{client: c, id: id, tokens: slices.Clone(tokens)}
}

// Bool returns the flag coerced to bool, or def on any miss.
func (e Evaluator) Bool(key string, def bool) bool {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseBool(s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "bool")
		return def
	}
	return v
}

// String returns the flag value, or def on any miss.
func (e Evaluator) String(key, def string) string {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	return s
}

// Int returns the flag coerced to int, or def on any miss.
func (e Evaluator) Int(key string, def int) int {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseInt[int](s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "int")
		return def
	}
	return v
}

// Float64 returns the flag coerced to float64, or def on any miss.
func (e Evaluator) Float64(key string, def float64) float64 {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseFloat[float64](s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "float64")
		return def
	}
	return v
}

// Duration returns the flag coerced to time.Duration, or def on any miss.
func (e Evaluator) Duration(key string, def time.Duration) time.Duration {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseDuration(s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "duration")
		return def
	}
	return v
}
