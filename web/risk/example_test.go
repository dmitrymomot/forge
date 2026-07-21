package risk_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dmitrymomot/forge/web/risk"
)

// Visit is the consumer's own input type — the engine is generic over it.
type Visit struct {
	UserAgent   string
	Fingerprint string
}

func Example() {
	// Scorers hold the fraud logic; the engine owns combining and gating.
	botUA := func(_ context.Context, v Visit) (float64, error) {
		if strings.Contains(strings.ToLower(v.UserAgent), "headless") {
			return 0.9, nil
		}
		return 0.1, nil
	}
	noFingerprint := func(_ context.Context, v Visit) (float64, error) {
		if v.Fingerprint == "" {
			return 0.6, nil
		}
		return 0, nil
	}

	engine, err := risk.New(
		risk.WithScorer(botUA),
		risk.WithScorer(noFingerprint),
		risk.WithGate[Visit](0.8), // default strategy Max: one strong signal trips
	)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	fmt.Println(engine.Check(ctx, Visit{UserAgent: "Mozilla/5.0", Fingerprint: "abc"}))

	err = engine.Check(ctx, Visit{UserAgent: "HeadlessChrome"})
	fmt.Println(errors.Is(err, risk.ErrFraud))
	if fraud, ok := errors.AsType[*risk.FraudError](err); ok {
		fmt.Printf("score %.1f from scorer %d\n", fraud.Score.Value, fraud.Score.PeakIdx)
	}
	// Output:
	// <nil>
	// true
	// score 0.9 from scorer 0
}
