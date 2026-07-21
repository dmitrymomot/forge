package formula_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/formula"
)

func revshareInputs(t *testing.T) map[string]decimal.Decimal {
	t.Helper()
	return map[string]decimal.Decimal{
		"bets":     d(t, "125000.00"),
		"wins":     d(t, "118400.50"),
		"carry_in": d(t, "-4000.00"),
	}
}

func TestCompile(t *testing.T) {
	t.Parallel()

	t.Run("invalid spec rejected", func(t *testing.T) {
		t.Parallel()
		_, err := formula.Compile(formula.Spec{})
		assert.ErrorIs(t, err, formula.ErrInvalidSpec)
	})

	t.Run("unknown func", func(t *testing.T) {
		t.Parallel()
		spec := validSpec()
		spec.Stages = append(spec.Stages, formula.Stage{Name: "capped", Func: "cap", Args: []string{"base"}})
		_, err := formula.Compile(spec)
		assert.ErrorIs(t, err, formula.ErrUnknownFunc)
	})

	identity := func(args []decimal.Decimal) (decimal.Decimal, error) { return args[0], nil }

	t.Run("bad registrations", func(t *testing.T) {
		t.Parallel()
		spec := validSpec()
		for name, opt := range map[string]formula.Option{
			"empty name": formula.WithFunc("", identity),
			"nil func":   formula.WithFunc("f", nil),
		} {
			_, err := formula.Compile(spec, opt)
			assert.ErrorIs(t, err, formula.ErrInvalidFunc, name)
		}
		_, err := formula.Compile(spec, formula.WithFunc("f", identity), formula.WithFunc("f", identity))
		assert.ErrorIs(t, err, formula.ErrInvalidFunc, "duplicate name")
	})

	t.Run("spec is copied at compile", func(t *testing.T) {
		t.Parallel()
		spec := validSpec()
		compiled, err := formula.Compile(spec)
		require.NoError(t, err)
		want := compiled.Fingerprint()

		spec.Stages[0].Terms[0].Coefficient = decimal.FromInt(999)
		spec.Version = "tampered"
		res, err := compiled.Eval(revshareInputs(t))
		require.NoError(t, err)
		assert.Equal(t, want, compiled.Fingerprint())
		assert.Equal(t, "v1", res.SpecVersion)
		assert.Equal(t, "6599.50", res.Stages[0].Value.String(), "eval unaffected by caller mutation")
	})

	t.Run("Spec returns a copy", func(t *testing.T) {
		t.Parallel()
		compiled, err := formula.Compile(validSpec())
		require.NoError(t, err)
		got := compiled.Spec()
		got.Stages[0].Name = "tampered"
		assert.Equal(t, validSpec().Fingerprint(), compiled.Spec().Fingerprint())
		assert.Equal(t, "v1", compiled.Version())
	})
}

func TestEvalRevshare(t *testing.T) {
	t.Parallel()

	spec := formula.Spec{
		Version: "revshare-2026-07",
		Stages: []formula.Stage{
			{
				Name: "ngr",
				Terms: []formula.Term{
					{Metric: "bets", Coefficient: decimal.FromInt(1)},
					{Metric: "wins", Coefficient: decimal.FromInt(-1)},
					{Metric: "bonus_cost", Coefficient: decimal.FromInt(-1)},
					{Metric: "provider_fees", Coefficient: decimal.FromInt(-1)},
				},
			},
			{
				Name: "base",
				Terms: []formula.Term{
					{Metric: "ngr", Coefficient: decimal.FromInt(1)},
					{Metric: "carry_in", Coefficient: decimal.FromInt(1)},
				},
				Round: &formula.Round{Scale: 2, Mode: decimal.HalfEven},
				Clamp: &formula.Clamp{Min: new(decimal.Zero)},
			},
		},
	}
	compiled, err := formula.Compile(spec)
	require.NoError(t, err)

	inputs := map[string]decimal.Decimal{
		"bets":          d(t, "125000.00"),
		"wins":          d(t, "118400.50"),
		"bonus_cost":    d(t, "2100.00"),
		"provider_fees": d(t, "1249.75"),
		"carry_in":      d(t, "-4000.00"),
	}
	res, err := compiled.Eval(inputs)
	require.NoError(t, err)

	assert.Equal(t, "revshare-2026-07", res.SpecVersion)
	assert.Equal(t, spec.Fingerprint(), res.SpecFingerprint)

	ngr, ok := res.Value("ngr")
	require.True(t, ok)
	assert.Equal(t, "3249.75", ngr.String())

	assert.Equal(t, "0", res.Final().String(), "negative base clamps to the Min bound")
	require.Len(t, res.Stages, 2)
	base := res.Stages[1]
	assert.Equal(t, "-750.25", base.Raw.String())
	assert.True(t, base.Clamped)

	require.Len(t, base.Terms, 2)
	assert.Equal(t, "ngr", base.Terms[0].Metric)
	assert.Equal(t, "3249.75", base.Terms[0].MetricValue.String())
	assert.Equal(t, "3249.75", base.Terms[0].Contribution.String())
	assert.Equal(t, "carry_in", base.Terms[1].Metric)
	assert.Equal(t, "-4000.00", base.Terms[1].Contribution.String())

	names := make([]string, len(res.Inputs))
	for i, in := range res.Inputs {
		names[i] = in.Name
	}
	assert.Equal(t, []string{"bets", "bonus_cost", "carry_in", "provider_fees", "wins"}, names, "inputs sorted by name")

	_, ok = res.Value("bets")
	assert.False(t, ok, "inputs are not derived metrics")
}

func TestEvalRoundThenClamp(t *testing.T) {
	t.Parallel()

	// Raw 10.007 rounds to 10.01, which exceeds max 10.005 — the clamp wins,
	// proving round-before-clamp ordering.
	maxBound := d(t, "10.005")
	spec := formula.Spec{
		Version: "v1",
		Stages: []formula.Stage{{
			Name:  "out",
			Terms: []formula.Term{{Metric: "in", Coefficient: decimal.FromInt(1)}},
			Round: &formula.Round{Scale: 2, Mode: decimal.HalfUp},
			Clamp: &formula.Clamp{Max: &maxBound},
		}},
	}
	compiled, err := formula.Compile(spec)
	require.NoError(t, err)

	res, err := compiled.Eval(map[string]decimal.Decimal{"in": d(t, "10.007")})
	require.NoError(t, err)
	assert.Equal(t, "10.005", res.Final().String())
	assert.True(t, res.Stages[0].Clamped)
	assert.Equal(t, "10.007", res.Stages[0].Raw.String())

	res, err = compiled.Eval(map[string]decimal.Decimal{"in": d(t, "9.994")})
	require.NoError(t, err)
	assert.Equal(t, "9.99", res.Final().String())
	assert.False(t, res.Stages[0].Clamped)
}

func TestEvalRoundingModes(t *testing.T) {
	t.Parallel()

	for mode, want := range map[decimal.RoundingMode]string{
		decimal.HalfEven: "2.34",
		decimal.HalfUp:   "2.35",
		decimal.Floor:    "2.34",
		decimal.Up:       "2.35",
	} {
		spec := formula.Spec{
			Version: "v1",
			Stages: []formula.Stage{{
				Name:  "out",
				Terms: []formula.Term{{Metric: "in", Coefficient: decimal.FromInt(1)}},
				Round: &formula.Round{Scale: 2, Mode: mode},
			}},
		}
		compiled, err := formula.Compile(spec)
		require.NoError(t, err)
		res, err := compiled.Eval(map[string]decimal.Decimal{"in": d(t, "2.345")})
		require.NoError(t, err)
		assert.Equal(t, want, res.Final().String(), mode.String())
	}
}

func TestEvalStageChainingUsesFinalValues(t *testing.T) {
	t.Parallel()

	// Stage one rounds 1.005 down to 1.00; stage two must see 1.00, not the
	// raw 1.005.
	spec := formula.Spec{
		Version: "v1",
		Stages: []formula.Stage{
			{
				Name:  "first",
				Terms: []formula.Term{{Metric: "in", Coefficient: decimal.FromInt(1)}},
				Round: &formula.Round{Scale: 2, Mode: decimal.Down},
			},
			{
				Name:  "second",
				Terms: []formula.Term{{Metric: "first", Coefficient: decimal.FromInt(100)}},
			},
		},
	}
	compiled, err := formula.Compile(spec)
	require.NoError(t, err)
	res, err := compiled.Eval(map[string]decimal.Decimal{"in": d(t, "1.005")})
	require.NoError(t, err)
	assert.Equal(t, "100.00", res.Final().String())
}

func TestEvalFuncStage(t *testing.T) {
	t.Parallel()

	cap := func(limit decimal.Decimal) formula.Func {
		return func(args []decimal.Decimal) (decimal.Decimal, error) {
			if args[0].Cmp(limit) > 0 {
				return limit, nil
			}
			return args[0], nil
		}
	}
	spec := formula.Spec{
		Version: "v1",
		Stages: []formula.Stage{
			{
				Name:  "gross",
				Terms: []formula.Term{{Metric: "usage", Coefficient: d(t, "0.10")}},
			},
			{
				Name:  "billable",
				Func:  "cap_monthly",
				Args:  []string{"gross"},
				Round: &formula.Round{Scale: 2, Mode: decimal.HalfEven},
			},
		},
	}
	compiled, err := formula.Compile(spec, formula.WithFunc("cap_monthly", cap(decimal.FromInt(500))))
	require.NoError(t, err)

	res, err := compiled.Eval(map[string]decimal.Decimal{"usage": decimal.FromInt(9000)})
	require.NoError(t, err)
	assert.Equal(t, "500.00", res.Final().String(), "capped and rounded")

	st := res.Stages[1]
	assert.Equal(t, "cap_monthly", st.Func)
	require.Len(t, st.Args, 1)
	assert.Equal(t, "gross", st.Args[0].Name)
	assert.Equal(t, "900.00", st.Args[0].Value.String())
	assert.Equal(t, "500", st.Raw.String())

	res, err = compiled.Eval(map[string]decimal.Decimal{"usage": decimal.FromInt(100)})
	require.NoError(t, err)
	assert.Equal(t, "10.00", res.Final().String(), "under the cap passes through")
}

func TestEvalFuncError(t *testing.T) {
	t.Parallel()

	cause := errors.New("negative usage")
	spec := formula.Spec{
		Version: "v1",
		Stages:  []formula.Stage{{Name: "out", Func: "guard", Args: []string{"in"}}},
	}
	compiled, err := formula.Compile(spec, formula.WithFunc("guard", func([]decimal.Decimal) (decimal.Decimal, error) {
		return decimal.Decimal{}, cause
	}))
	require.NoError(t, err)

	_, err = compiled.Eval(map[string]decimal.Decimal{"in": decimal.FromInt(1)})
	assert.ErrorIs(t, err, formula.ErrFuncFailed)
	assert.ErrorIs(t, err, cause, "the func's own error stays matchable")
}

func TestEvalInputErrors(t *testing.T) {
	t.Parallel()

	compiled, err := formula.Compile(validSpec())
	require.NoError(t, err)

	t.Run("missing input", func(t *testing.T) {
		t.Parallel()
		_, err := compiled.Eval(map[string]decimal.Decimal{"bets": decimal.FromInt(1)})
		assert.ErrorIs(t, err, formula.ErrUnknownMetric)
		assert.ErrorContains(t, err, `"wins"`)
	})

	t.Run("input collides with stage", func(t *testing.T) {
		t.Parallel()
		in := revshareInputs(t)
		in["ngr"] = decimal.FromInt(7)
		_, err := compiled.Eval(in)
		assert.ErrorIs(t, err, formula.ErrMetricCollision)
	})

	t.Run("extra inputs recorded, not rejected", func(t *testing.T) {
		t.Parallel()
		in := revshareInputs(t)
		in["unused"] = decimal.FromInt(42)
		res, err := compiled.Eval(in)
		require.NoError(t, err)
		_, ok := res.Value("unused")
		assert.False(t, ok)
		var found bool
		for _, iv := range res.Inputs {
			found = found || iv.Name == "unused"
		}
		assert.True(t, found, "unused input still snapshotted for the audit record")
	})
}

func TestEvalDeterministicByteMatch(t *testing.T) {
	t.Parallel()

	compiled, err := formula.Compile(validSpec())
	require.NoError(t, err)

	first, err := compiled.Eval(revshareInputs(t))
	require.NoError(t, err)
	second, err := compiled.Eval(revshareInputs(t))
	require.NoError(t, err)

	a, err := json.Marshal(first)
	require.NoError(t, err)
	b, err := json.Marshal(second)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b), "recomputes byte-match")
}

func TestResultFinalEmpty(t *testing.T) {
	t.Parallel()
	assert.True(t, formula.Result{}.Final().IsZero())
	_, ok := formula.Result{}.Value("x")
	assert.False(t, ok)
}
