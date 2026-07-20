package formula_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/formula"
)

func d(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	v, err := decimal.Parse(s)
	require.NoError(t, err)
	return v
}

func validSpec() formula.Spec {
	return formula.Spec{
		Version: "v1",
		Stages: []formula.Stage{
			{
				Name: "ngr",
				Terms: []formula.Term{
					{Metric: "bets", Coefficient: decimal.FromInt(1)},
					{Metric: "wins", Coefficient: decimal.FromInt(-1)},
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
}

func TestSpecValidate(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, validSpec().Validate())
	})

	mutations := map[string]func(*formula.Spec){
		"empty version":        func(s *formula.Spec) { s.Version = "" },
		"no stages":            func(s *formula.Spec) { s.Stages = nil },
		"empty stage name":     func(s *formula.Spec) { s.Stages[0].Name = "" },
		"duplicate stage name": func(s *formula.Spec) { s.Stages[1].Name = "ngr" },
		"terms and func":       func(s *formula.Spec) { s.Stages[0].Func = "f" },
		"neither terms nor func": func(s *formula.Spec) {
			s.Stages[0].Terms = nil
		},
		"args without func": func(s *formula.Spec) { s.Stages[0].Args = []string{"bets"} },
		"empty term metric": func(s *formula.Spec) { s.Stages[0].Terms[0].Metric = "" },
		"self reference": func(s *formula.Spec) {
			s.Stages[0].Terms[0].Metric = "ngr"
		},
		"forward reference": func(s *formula.Spec) {
			s.Stages[0].Terms[0].Metric = "base"
		},
		"empty clamp": func(s *formula.Spec) { s.Stages[1].Clamp = &formula.Clamp{} },
		"clamp min above max": func(s *formula.Spec) {
			one, ten := decimal.FromInt(1), decimal.FromInt(10)
			s.Stages[1].Clamp = &formula.Clamp{Min: &ten, Max: &one}
		},
		"negative round scale": func(s *formula.Spec) { s.Stages[1].Round.Scale = -1 },
		"unknown rounding mode": func(s *formula.Spec) {
			s.Stages[1].Round.Mode = decimal.RoundingMode(99)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := validSpec()
			mutate(&spec)
			assert.ErrorIs(t, spec.Validate(), formula.ErrInvalidSpec)
		})
	}

	t.Run("func stage validation", func(t *testing.T) {
		t.Parallel()
		spec := formula.Spec{
			Version: "v1",
			Stages: []formula.Stage{
				{Name: "a", Terms: []formula.Term{{Metric: "x", Coefficient: decimal.FromInt(1)}}},
				{Name: "b", Func: "f", Args: []string{"a", "y"}},
			},
		}
		assert.NoError(t, spec.Validate())

		spec.Stages[1].Args = []string{""}
		assert.ErrorIs(t, spec.Validate(), formula.ErrInvalidSpec)

		spec.Stages[1].Args = []string{"b"}
		assert.ErrorIs(t, spec.Validate(), formula.ErrInvalidSpec, "self reference via arg")
	})

	t.Run("clamp min equal max is valid", func(t *testing.T) {
		t.Parallel()
		spec := validSpec()
		five := decimal.FromInt(5)
		spec.Stages[1].Clamp = &formula.Clamp{Min: &five, Max: &five}
		assert.NoError(t, spec.Validate())
	})
}

func TestSpecFingerprint(t *testing.T) {
	t.Parallel()

	t.Run("deterministic", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, validSpec().Fingerprint(), validSpec().Fingerprint())
	})

	t.Run("golden", func(t *testing.T) {
		t.Parallel()
		// Frozen canonical encoding: this hash must never change across
		// releases, or stored fingerprints stop verifying.
		spec := formula.Spec{
			Version: "golden-v1",
			Stages: []formula.Stage{
				{
					Name:  "out",
					Terms: []formula.Term{{Metric: "in", Coefficient: decimal.MustParse("1.50")}},
					Round: &formula.Round{Scale: 2, Mode: decimal.HalfUp},
					Clamp: &formula.Clamp{Min: new(decimal.Zero)},
				},
			},
		}
		assert.Equal(t, "83daa3ac17d9772d5d4689b16da2d69bf5ebb75bce5c6b4057895ff87b398d77", spec.Fingerprint())
	})

	changes := map[string]func(*formula.Spec){
		"version":           func(s *formula.Spec) { s.Version = "v2" },
		"stage name":        func(s *formula.Spec) { s.Stages[0].Name = "ngr2" },
		"term metric":       func(s *formula.Spec) { s.Stages[0].Terms[0].Metric = "stakes" },
		"coefficient scale": func(s *formula.Spec) { s.Stages[0].Terms[0].Coefficient = decimal.MustParse("1.0") },
		"term order": func(s *formula.Spec) {
			s.Stages[0].Terms[0], s.Stages[0].Terms[1] = s.Stages[0].Terms[1], s.Stages[0].Terms[0]
		},
		"drop clamp":  func(s *formula.Spec) { s.Stages[1].Clamp = nil },
		"round mode":  func(s *formula.Spec) { s.Stages[1].Round.Mode = decimal.Floor },
		"round scale": func(s *formula.Spec) { s.Stages[1].Round.Scale = 4 },
	}
	for name, mutate := range changes {
		t.Run("changes on "+name, func(t *testing.T) {
			t.Parallel()
			spec := validSpec()
			mutate(&spec)
			assert.NotEqual(t, validSpec().Fingerprint(), spec.Fingerprint())
		})
	}
}

func TestSpecJSONRoundTrip(t *testing.T) {
	t.Parallel()

	spec := validSpec()
	spec.Stages = append(spec.Stages, formula.Stage{Name: "capped", Func: "cap", Args: []string{"base"}})
	raw, err := json.Marshal(spec)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"mode":"half_even"`, "rounding mode serializes as text")

	var back formula.Spec
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, spec.Fingerprint(), back.Fingerprint(), "round-trip preserves identity")

	var bad formula.Spec
	err = json.Unmarshal([]byte(`{"version":"v1","stages":[{"name":"a","round":{"scale":2,"mode":"sideways"}}]}`), &bad)
	assert.ErrorIs(t, err, decimal.ErrSyntax)
}
