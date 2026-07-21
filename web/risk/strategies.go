package risk

import (
	"fmt"
	"math"
)

// Strategy combines per-scorer scores into one value in [0, 1]. New
// guarantees at least one score. Built-ins: Max (default) and Mean; a
// weighted average is configured with WithWeights, not a Strategy, so its
// arity is validated in New. A custom Strategy returning NaN or a value
// outside [0, 1] fails the check closed.
type Strategy func(scores []float64) float64

// Max returns the highest score — the default strategy. Fraud signals are
// not additive: one strong signal must not be diluted by weak ones.
func Max(scores []float64) float64 {
	peak := scores[0]
	for _, v := range scores[1:] {
		if v > peak {
			peak = v
		}
	}
	return peak
}

// Mean returns the arithmetic mean of the scores.
func Mean(scores []float64) float64 {
	sum := 0.0
	for _, v := range scores {
		sum += v
	}
	return clamp01(sum / float64(len(scores)))
}

// normalizeWeights validates WithWeights values against the scorer count and
// returns them scaled to sum to 1, so Weighted(1, 3) means 25%/75%.
func normalizeWeights(weights []float64, scorers int) ([]float64, error) {
	if len(weights) != scorers {
		return nil, fmt.Errorf("%w: %d weights for %d scorers", ErrInvalidWeights, len(weights), scorers)
	}
	sum := 0.0
	for i, w := range weights {
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 {
			return nil, fmt.Errorf("%w: weight %d is %v", ErrInvalidWeights, i, w)
		}
		sum += w
	}
	if sum <= 0 {
		return nil, fmt.Errorf("%w: weights sum to zero", ErrInvalidWeights)
	}
	normalized := make([]float64, len(weights))
	for i, w := range weights {
		normalized[i] = w / sum
	}
	return normalized, nil
}
