package risk

import (
	"context"
	"slices"
)

type config[T any] struct {
	onFraud     func(ctx context.Context, input T, score Score) error
	strategy    Strategy
	scorers     []Scorer[T]
	weights     []float64
	gate        float64
	strategySet bool
	weightsSet  bool
}

// Option configures New.
type Option[T any] func(*config[T])

// WithScorer adds a scorer. Repeatable; scorers run in registration order and
// New requires at least one.
func WithScorer[T any](s Scorer[T]) Option[T] {
	return func(c *config[T]) { c.scorers = append(c.scorers, s) }
}

// WithGate sets the trip threshold. Required; must be in (0, 1]. A combined
// score >= threshold trips the gate (boundary inclusive).
func WithGate[T any](threshold float64) Option[T] {
	return func(c *config[T]) { c.gate = threshold }
}

// WithStrategy replaces the default Max combining strategy with Mean or a
// custom Strategy. Mutually exclusive with WithWeights.
func WithStrategy[T any](st Strategy) Option[T] {
	return func(c *config[T]) {
		c.strategy = st
		c.strategySet = true
	}
}

// WithWeights configures a weighted-average strategy. Weight count must equal
// the scorer count (positional, validated by New), weights must be
// non-negative with a positive sum, and are normalized internally so
// WithWeights(1, 3) means 25%/75%. Mutually exclusive with WithStrategy.
func WithWeights[T any](weights ...float64) Option[T] {
	return func(c *config[T]) {
		c.weights = slices.Clone(weights)
		c.weightsSet = true
	}
}

// OnFraud sets the handler run on a gate trip in place of returning
// *FraudError. Its return decides the outcome: nil proceeds (shadow mode,
// divert-and-allow), an error blocks Check with that error verbatim.
func OnFraud[T any](h func(ctx context.Context, input T, score Score) error) Option[T] {
	return func(c *config[T]) { c.onFraud = h }
}
