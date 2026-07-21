package risk_test

import (
	"context"
	"math"
	"testing"

	"github.com/dmitrymomot/forge/web/risk"
)

// FuzzWeights pins the weighted-average invariant: for any weight vector New
// accepts and any valid scores, the combined value stays in [0, 1] with no
// error and no NaN.
func FuzzWeights(f *testing.F) {
	f.Add(1.0, 3.0, 0.0, 0.4, 0.8, 1.0)
	f.Add(0.001, 1000.0, 5.0, 0.0, 1.0, 0.5)
	f.Add(1.0, 1.0, 1.0, 0.3333, 0.6667, 0.9999)
	f.Fuzz(func(t *testing.T, w1, w2, w3, s1, s2, s3 float64) {
		valid := func(v float64) bool { return v >= 0 && v <= 1 }
		for _, s := range []float64{s1, s2, s3} {
			if !valid(s) {
				t.Skip()
			}
		}
		scores := []float64{s1, s2, s3}
		e, err := risk.New(
			risk.WithScorer(func(context.Context, int) (float64, error) { return scores[0], nil }),
			risk.WithScorer(func(context.Context, int) (float64, error) { return scores[1], nil }),
			risk.WithScorer(func(context.Context, int) (float64, error) { return scores[2], nil }),
			risk.WithGate[int](1),
			risk.WithWeights[int](w1, w2, w3),
		)
		if err != nil {
			t.Skip() // New rejected the weights — rejection is its own tested contract
		}
		sc, err := e.Score(context.Background(), 0)
		if err != nil {
			t.Fatalf("Score() = %v for weights %v scores %v", err, []float64{w1, w2, w3}, scores)
		}
		if math.IsNaN(sc.Value) || sc.Value < 0 || sc.Value > 1 {
			t.Fatalf("combined value %v out of [0,1] for weights %v scores %v", sc.Value, []float64{w1, w2, w3}, scores)
		}
	})
}
