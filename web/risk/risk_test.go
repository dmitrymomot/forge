package risk_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/dmitrymomot/forge/web/risk"
)

// constScorer returns v unconditionally.
func constScorer(v float64) risk.Scorer[string] {
	return func(context.Context, string) (float64, error) { return v, nil }
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	scorer := constScorer(0.5)

	tests := []struct {
		name string
		opts []risk.Option[string]
		want error
	}{
		{"no scorers", []risk.Option[string]{risk.WithGate[string](0.8)}, risk.ErrNoScorers},
		{"nil scorer", []risk.Option[string]{risk.WithScorer[string](nil), risk.WithGate[string](0.8)}, risk.ErrNilScorer},
		{"missing gate", []risk.Option[string]{risk.WithScorer(scorer)}, risk.ErrInvalidGate},
		{"gate zero", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](0)}, risk.ErrInvalidGate},
		{"gate negative", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](-0.1)}, risk.ErrInvalidGate},
		{"gate above one", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](1.1)}, risk.ErrInvalidGate},
		{"gate NaN", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](math.NaN())}, risk.ErrInvalidGate},
		{"nil strategy", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](0.8), risk.WithStrategy[string](nil)}, risk.ErrNilStrategy},
		{"strategy conflict", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](0.8), risk.WithStrategy[string](risk.Mean), risk.WithWeights[string](1)}, risk.ErrStrategyConflict},
		{"weights arity", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](0.8), risk.WithWeights[string](1, 2)}, risk.ErrInvalidWeights},
		{"weight negative", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](0.8), risk.WithWeights[string](-1)}, risk.ErrInvalidWeights},
		{"weight NaN", []risk.Option[string]{risk.WithScorer(scorer), risk.WithGate[string](0.8), risk.WithWeights[string](math.NaN())}, risk.ErrInvalidWeights},
		{"weights zero sum", []risk.Option[string]{risk.WithScorer(scorer), risk.WithScorer(scorer), risk.WithGate[string](0.8), risk.WithWeights[string](0, 0)}, risk.ErrInvalidWeights},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := risk.New(tt.opts...)
			if !errors.Is(err, tt.want) {
				t.Fatalf("New() error = %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		e, err := risk.New(risk.WithScorer(scorer), risk.WithGate[string](1))
		if err != nil || e == nil {
			t.Fatalf("New() = %v, %v; want engine, nil", e, err)
		}
	})
}

func TestCheckPass(t *testing.T) {
	t.Parallel()
	e, err := risk.New(
		risk.WithScorer(constScorer(0.1)),
		risk.WithScorer(constScorer(0.79)),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(context.Background(), "visit"); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

func TestCheckTripNoHandler(t *testing.T) {
	t.Parallel()
	e, err := risk.New(
		risk.WithScorer(constScorer(0.2)),
		risk.WithScorer(constScorer(0.9)),
		risk.WithScorer(constScorer(0.5)),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = e.Check(context.Background(), "visit")
	if !errors.Is(err, risk.ErrFraud) {
		t.Fatalf("Check() = %v, want ErrFraud match", err)
	}
	fraud, ok := errors.AsType[*risk.FraudError](err)
	if !ok {
		t.Fatalf("Check() = %T, want *FraudError", err)
	}
	sc := fraud.Score
	if sc.Value != 0.9 || sc.Peak != 0.9 || sc.PeakIdx != 1 {
		t.Errorf("Score = %+v, want Value 0.9, Peak 0.9, PeakIdx 1", sc)
	}
	want := []float64{0.2, 0.9, 0.5}
	if len(sc.Scores) != len(want) {
		t.Fatalf("Scores = %v, want %v", sc.Scores, want)
	}
	for i, v := range want {
		if sc.Scores[i] != v {
			t.Errorf("Scores[%d] = %v, want %v", i, sc.Scores[i], v)
		}
	}
}

func TestCheckGateBoundaryInclusive(t *testing.T) {
	t.Parallel()
	e, err := risk.New(risk.WithScorer(constScorer(0.8)), risk.WithGate[string](0.8))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(context.Background(), "visit"); !errors.Is(err, risk.ErrFraud) {
		t.Fatalf("Check() at score == gate = %v, want ErrFraud match", err)
	}
}

func TestOnFraudNilProceeds(t *testing.T) {
	t.Parallel()
	called := false
	e, err := risk.New(
		risk.WithScorer(constScorer(0.9)),
		risk.WithGate[string](0.8),
		risk.OnFraud(func(_ context.Context, input string, sc risk.Score) error {
			called = true
			if input != "visit" {
				t.Errorf("handler input = %q, want %q", input, "visit")
			}
			if sc.Value != 0.9 {
				t.Errorf("handler score = %v, want 0.9", sc.Value)
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(context.Background(), "visit"); err != nil {
		t.Fatalf("Check() with nil-returning handler = %v, want nil", err)
	}
	if !called {
		t.Fatal("OnFraud handler was not called")
	}
}

func TestOnFraudErrorBlocksVerbatim(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("diverted")
	e, err := risk.New(
		risk.WithScorer(constScorer(0.9)),
		risk.WithGate[string](0.8),
		risk.OnFraud(func(context.Context, string, risk.Score) error { return sentinel }),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = e.Check(context.Background(), "visit")
	if err != sentinel { //nolint:errorlint // verbatim identity is the contract
		t.Fatalf("Check() = %v, want handler error verbatim", err)
	}
	if errors.Is(err, risk.ErrFraud) {
		t.Fatal("handler error must not be wrapped in FraudError")
	}
}

func TestOnFraudNotCalledBelowGate(t *testing.T) {
	t.Parallel()
	e, err := risk.New(
		risk.WithScorer(constScorer(0.5)),
		risk.WithGate[string](0.8),
		risk.OnFraud(func(context.Context, string, risk.Score) error {
			t.Error("OnFraud called below gate")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(context.Background(), "visit"); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

func TestAllScorersRunNoShortCircuit(t *testing.T) {
	t.Parallel()
	var order []int
	scorer := func(i int, v float64) risk.Scorer[string] {
		return func(context.Context, string) (float64, error) {
			order = append(order, i)
			return v, nil
		}
	}
	e, err := risk.New(
		risk.WithScorer(scorer(0, 0.95)), // already >= gate
		risk.WithScorer(scorer(1, 0.1)),
		risk.WithScorer(scorer(2, 0.2)),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(context.Background(), "visit"); !errors.Is(err, risk.ErrFraud) {
		t.Fatalf("Check() = %v, want ErrFraud match", err)
	}
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Fatalf("scorer call order = %v, want [0 1 2]", order)
	}
}

func TestScorerErrorFailsClosed(t *testing.T) {
	t.Parallel()
	boom := errors.New("lookup down")
	e, err := risk.New(
		risk.WithScorer(constScorer(0.1)),
		risk.WithScorer(func(context.Context, string) (float64, error) { return 0, boom }),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = e.Check(context.Background(), "visit")
	if !errors.Is(err, boom) {
		t.Fatalf("Check() = %v, want wrapped scorer error", err)
	}
	if errors.Is(err, risk.ErrFraud) {
		t.Fatal("scorer error must not match ErrFraud")
	}
}

func TestInvalidScorerOutputFailsClosed(t *testing.T) {
	t.Parallel()
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.1, 1.1} {
		e, err := risk.New(risk.WithScorer(constScorer(bad)), risk.WithGate[string](0.8))
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Check(context.Background(), "visit"); !errors.Is(err, risk.ErrInvalidScore) {
			t.Errorf("Check() with scorer output %v = %v, want ErrInvalidScore", bad, err)
		}
	}
}

func TestInvalidCustomStrategyOutputFailsClosed(t *testing.T) {
	t.Parallel()
	e, err := risk.New(
		risk.WithScorer(constScorer(0.5)),
		risk.WithGate[string](0.8),
		risk.WithStrategy[string](func([]float64) float64 { return math.NaN() }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(context.Background(), "visit"); !errors.Is(err, risk.ErrInvalidScore) {
		t.Fatalf("Check() with NaN strategy = %v, want ErrInvalidScore", err)
	}
}

func TestContextCancellationBetweenScorers(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	secondRan := false
	e, err := risk.New(
		risk.WithScorer(func(context.Context, string) (float64, error) {
			cancel()
			return 0.1, nil
		}),
		risk.WithScorer(func(context.Context, string) (float64, error) {
			secondRan = true
			return 0.1, nil
		}),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Check(ctx, "visit"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check() = %v, want context.Canceled", err)
	}
	if secondRan {
		t.Fatal("second scorer ran after cancellation")
	}
}

func TestScoreNoGating(t *testing.T) {
	t.Parallel()
	e, err := risk.New(
		risk.WithScorer(constScorer(0.95)), // above gate — Score must not care
		risk.WithScorer(constScorer(0.3)),
		risk.WithGate[string](0.8),
	)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := e.Score(context.Background(), "visit")
	if err != nil {
		t.Fatalf("Score() = %v, want nil", err)
	}
	if sc.Value != 0.95 || sc.Peak != 0.95 || sc.PeakIdx != 0 {
		t.Errorf("Score = %+v, want Value 0.95, Peak 0.95, PeakIdx 0", sc)
	}
}

func TestStrategies(t *testing.T) {
	t.Parallel()

	t.Run("max default", func(t *testing.T) {
		t.Parallel()
		e, err := risk.New(
			risk.WithScorer(constScorer(0.3)),
			risk.WithScorer(constScorer(0.6)),
			risk.WithGate[string](0.5),
		)
		if err != nil {
			t.Fatal(err)
		}
		sc, err := e.Score(context.Background(), "v")
		if err != nil || sc.Value != 0.6 {
			t.Fatalf("Score() = %+v, %v; want Value 0.6 (Max default)", sc, err)
		}
	})

	t.Run("mean", func(t *testing.T) {
		t.Parallel()
		e, err := risk.New(
			risk.WithScorer(constScorer(0.2)),
			risk.WithScorer(constScorer(0.6)),
			risk.WithGate[string](0.5),
			risk.WithStrategy[string](risk.Mean),
		)
		if err != nil {
			t.Fatal(err)
		}
		sc, err := e.Score(context.Background(), "v")
		if err != nil || math.Abs(sc.Value-0.4) > 1e-12 {
			t.Fatalf("Score() = %+v, %v; want Value 0.4 (Mean)", sc, err)
		}
	})

	t.Run("weights normalized", func(t *testing.T) {
		t.Parallel()
		// Weights 1,3 normalize to 0.25/0.75: 0.25*0.4 + 0.75*0.8 = 0.7.
		e, err := risk.New(
			risk.WithScorer(constScorer(0.4)),
			risk.WithScorer(constScorer(0.8)),
			risk.WithGate[string](0.9),
			risk.WithWeights[string](1, 3),
		)
		if err != nil {
			t.Fatal(err)
		}
		sc, err := e.Score(context.Background(), "v")
		if err != nil || math.Abs(sc.Value-0.7) > 1e-12 {
			t.Fatalf("Score() = %+v, %v; want Value 0.7", sc, err)
		}
	})

	t.Run("custom", func(t *testing.T) {
		t.Parallel()
		e, err := risk.New(
			risk.WithScorer(constScorer(0.3)),
			risk.WithScorer(constScorer(0.5)),
			risk.WithGate[string](0.5),
			risk.WithStrategy[string](func(scores []float64) float64 { return scores[0] }),
		)
		if err != nil {
			t.Fatal(err)
		}
		sc, err := e.Score(context.Background(), "v")
		if err != nil || sc.Value != 0.3 {
			t.Fatalf("Score() = %+v, %v; want Value 0.3 (custom)", sc, err)
		}
	})
}

func TestFraudErrorMessage(t *testing.T) {
	t.Parallel()
	e, err := risk.New(risk.WithScorer(constScorer(0.9)), risk.WithGate[string](0.8))
	if err != nil {
		t.Fatal(err)
	}
	err = e.Check(context.Background(), "visit")
	if err == nil || err.Error() == "" {
		t.Fatalf("FraudError message empty, err = %v", err)
	}
}
