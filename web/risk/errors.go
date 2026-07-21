package risk

import (
	"errors"
	"fmt"
)

var (
	// ErrFraud matches (via errors.Is) the *FraudError returned by Check when
	// the combined score reaches the gate and no OnFraud handler is set.
	ErrFraud = errors.New("risk: fraud detected")

	// ErrNoScorers is returned by New when no WithScorer option was given.
	ErrNoScorers = errors.New("risk: no scorers")

	// ErrNilScorer is returned by New for a nil scorer.
	ErrNilScorer = errors.New("risk: nil scorer")

	// ErrInvalidGate is returned by New when the gate is missing or outside
	// (0, 1]. A gate of 0 would trip on every input.
	ErrInvalidGate = errors.New("risk: gate must be in (0, 1]")

	// ErrNilStrategy is returned by New for WithStrategy(nil).
	ErrNilStrategy = errors.New("risk: nil strategy")

	// ErrInvalidWeights is returned by New when WithWeights arity does not
	// match the scorer count, a weight is negative or NaN, or all weights are
	// zero.
	ErrInvalidWeights = errors.New("risk: invalid weights")

	// ErrStrategyConflict is returned by New when both WithStrategy and
	// WithWeights are set.
	ErrStrategyConflict = errors.New("risk: WithStrategy and WithWeights are mutually exclusive")

	// ErrInvalidScore is returned by Check and Score when a scorer or a custom
	// strategy produces NaN, an infinity, or a value outside [0, 1]. Broken
	// scorers fail closed instead of being clamped.
	ErrInvalidScore = errors.New("risk: score outside [0, 1]")
)

// FraudError is returned by Check on a gate trip with no OnFraud handler. It
// matches errors.Is(err, ErrFraud) and carries the full Score attribution so
// callers can log which scorer peaked.
type FraudError struct {
	Score Score
}

// Error implements error.
func (e *FraudError) Error() string {
	return fmt.Sprintf("risk: fraud detected: score %.4g, peak %.4g from scorer %d", e.Score.Value, e.Score.Peak, e.Score.PeakIdx)
}

// Is reports true for ErrFraud, so errors.Is(err, ErrFraud) matches any
// *FraudError.
func (e *FraudError) Is(target error) bool { return target == ErrFraud }
