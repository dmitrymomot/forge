package risk

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Scorer returns the fraud probability of input in [0, 1]. A non-nil error or
// an out-of-range value fails the check closed. Implementations must be safe
// for concurrent use.
type Scorer[T any] func(ctx context.Context, input T) (float64, error)

// Score is the combined result of one evaluation, with per-scorer
// attribution. Indexes refer to scorer registration order.
type Score struct {
	// Scores holds every scorer's value in registration order.
	Scores []float64
	// Value is the strategy-combined score compared against the gate.
	Value float64
	// Peak is the highest individual scorer value.
	Peak float64
	// PeakIdx is the index of the scorer that produced Peak.
	PeakIdx int
}

// Engine gates inputs on their combined fraud score. Build with New;
// immutable and concurrent-safe afterwards.
type Engine[T any] struct {
	// scratch pools the per-Check scores buffer so the pass path — every
	// legitimate request — allocates nothing; attribution is copied out only
	// on a trip (see bench_test.go).
	scratch  sync.Pool
	onFraud  func(ctx context.Context, input T, score Score) error
	strategy Strategy
	scorers  []Scorer[T]
	weights  []float64 // normalized; nil unless WithWeights
	gate     float64
}

// New validates the options and builds an Engine. At least one WithScorer and
// a WithGate in (0, 1] are required. Errors: ErrNoScorers, ErrNilScorer,
// ErrInvalidGate, ErrNilStrategy, ErrInvalidWeights, ErrStrategyConflict.
func New[T any](opts ...Option[T]) (*Engine[T], error) {
	var cfg config[T]
	for _, o := range opts {
		o(&cfg)
	}
	if len(cfg.scorers) == 0 {
		return nil, ErrNoScorers
	}
	for i, s := range cfg.scorers {
		if s == nil {
			return nil, fmt.Errorf("%w: index %d", ErrNilScorer, i)
		}
	}
	if !(cfg.gate > 0 && cfg.gate <= 1) { // NaN fails both comparisons
		return nil, fmt.Errorf("%w: got %v", ErrInvalidGate, cfg.gate)
	}
	if cfg.strategySet && cfg.weightsSet {
		return nil, ErrStrategyConflict
	}
	if cfg.strategySet && cfg.strategy == nil {
		return nil, ErrNilStrategy
	}
	n := len(cfg.scorers)
	e := &Engine[T]{
		onFraud:  cfg.onFraud,
		strategy: cfg.strategy,
		scorers:  cfg.scorers,
		gate:     cfg.gate,
		scratch: sync.Pool{New: func() any {
			s := make([]float64, n)
			return &s
		}},
	}
	if cfg.weightsSet {
		w, err := normalizeWeights(cfg.weights, len(cfg.scorers))
		if err != nil {
			return nil, err
		}
		e.weights = w
	}
	if e.strategy == nil && e.weights == nil {
		e.strategy = Max
	}
	return e, nil
}

// Check evaluates input and gates the combined score. It returns nil to
// proceed. On score >= gate: with no OnFraud handler it returns a *FraudError
// (errors.Is-matchable against ErrFraud); with a handler it returns the
// handler's result verbatim — nil proceeds (shadow mode), an error blocks.
// Scorer errors, invalid scores, and context cancellation fail closed with
// the underlying error.
func (e *Engine[T]) Check(ctx context.Context, input T) error {
	buf := e.scratch.Get().(*[]float64)
	sc, err := e.evaluate(ctx, input, *buf)
	if err != nil || sc.Value < e.gate {
		e.scratch.Put(buf)
		return err
	}
	sc.Scores = slices.Clone(sc.Scores) // detach attribution from the pooled buffer
	e.scratch.Put(buf)
	if e.onFraud != nil {
		return e.onFraud(ctx, input, sc)
	}
	return &FraudError{Score: sc}
}

// Score evaluates input and returns the combined score without gating — for
// telemetry, hit tagging, and tuning the gate in shadow mode.
func (e *Engine[T]) Score(ctx context.Context, input T) (Score, error) {
	return e.evaluate(ctx, input, make([]float64, len(e.scorers)))
}

// evaluate runs every scorer in registration order — all of them, no
// short-circuit, so attribution is complete and consumer scorer side effects
// do not vary with registration order — and combines the results into scores,
// which the returned Score aliases.
func (e *Engine[T]) evaluate(ctx context.Context, input T, scores []float64) (Score, error) {
	peak, peakIdx := -1.0, 0
	for i, s := range e.scorers {
		if err := ctx.Err(); err != nil {
			return Score{}, err
		}
		v, err := s(ctx, input)
		if err != nil {
			return Score{}, fmt.Errorf("risk: scorer %d: %w", i, err)
		}
		if !validScore(v) {
			return Score{}, fmt.Errorf("%w: scorer %d returned %v", ErrInvalidScore, i, v)
		}
		scores[i] = v
		if v > peak {
			peak, peakIdx = v, i
		}
	}
	var value float64
	if e.weights != nil {
		for i, v := range scores {
			value += e.weights[i] * v
		}
		value = clamp01(value) // float rounding only; inputs are validated
	} else {
		value = e.strategy(scores)
		if !validScore(value) {
			return Score{}, fmt.Errorf("%w: strategy returned %v", ErrInvalidScore, value)
		}
	}
	return Score{Scores: scores, Value: value, Peak: peak, PeakIdx: peakIdx}, nil
}

// validScore reports whether v is a usable probability. NaN and infinities
// fail both comparisons.
func validScore(v float64) bool { return v >= 0 && v <= 1 }

func clamp01(v float64) float64 {
	return min(1, max(0, v))
}
