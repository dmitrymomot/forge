package formula_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/formula"
)

func benchSpec() formula.Spec {
	one := decimal.FromInt(1)
	minusOne := decimal.FromInt(-1)
	return formula.Spec{
		Version: "bench-v1",
		Stages: []formula.Stage{
			{
				Name: "ngr",
				Terms: []formula.Term{
					{Metric: "bets", Coefficient: one},
					{Metric: "wins", Coefficient: minusOne},
					{Metric: "bonus_cost", Coefficient: minusOne},
					{Metric: "provider_fees", Coefficient: minusOne},
				},
			},
			{
				Name: "base",
				Terms: []formula.Term{
					{Metric: "ngr", Coefficient: one},
					{Metric: "carry_in", Coefficient: one},
				},
				Round: &formula.Round{Scale: 2, Mode: decimal.HalfEven},
				Clamp: &formula.Clamp{Min: new(decimal.Zero)},
			},
			{
				Name: "commission",
				Terms: []formula.Term{
					{Metric: "base", Coefficient: decimal.MustParse("0.35")},
				},
				Round: &formula.Round{Scale: 2, Mode: decimal.HalfEven},
			},
		},
	}
}

func benchInputs() map[string]decimal.Decimal {
	return map[string]decimal.Decimal{
		"bets":          decimal.MustParse("125000.00"),
		"wins":          decimal.MustParse("118400.50"),
		"bonus_cost":    decimal.MustParse("2100.00"),
		"provider_fees": decimal.MustParse("1249.75"),
		"carry_in":      decimal.MustParse("-1500.00"),
	}
}

func BenchmarkCompile(b *testing.B) {
	spec := benchSpec()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := formula.Compile(spec); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEval(b *testing.B) {
	compiled, err := formula.Compile(benchSpec())
	if err != nil {
		b.Fatal(err)
	}
	inputs := benchInputs()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := compiled.Eval(inputs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFingerprint(b *testing.B) {
	spec := benchSpec()
	b.ReportAllocs()
	for b.Loop() {
		_ = spec.Fingerprint()
	}
}
