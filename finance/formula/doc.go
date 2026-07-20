// Package formula evaluates formulas kept as structured, versioned data —
// never text: a Spec of named derived metrics, each an ordered list of
// (metric, decimal coefficient) terms over inputs or prior stages plus an
// optional clamp and rounding, evaluated exactly in core/decimal. It derives
// the bases (NGR, billable usage, commission bases) that tariff rates and a
// ledger posts.
//
// Every evaluation returns a Result explanation record — spec version and
// fingerprint, every input, every stage, every term's contribution — the
// statement line item and the dispute answer. Specs are immutable once
// referenced: store them verbatim, mint a new Version for any change, and a
// recompute of the same spec over the same inputs byte-matches the original
// (json.Marshal of Result is deterministic).
//
// Hard anti-scope: no string parsing, no conditionals, no user-typed
// expressions (they evaluate in float and can't explain a payout). Anything
// beyond staged linear terms + clamp is a registered Go function — for fixed
// deal shapes the documented default is named Go functions with per-deal
// parameters closed over as data (see WithFunc).
//
// # Usage — revenue-share NGR with negative carry-over
//
// NGR is a linear combination of period totals; the commission base adds the
// (negative or zero) carry-in and clamps at zero, so a losing month carries
// forward instead of going negative — carry-over policy stays in the
// consumer's domain table, the formula just takes it as an input:
//
//	spec := formula.Spec{
//		Version: "revshare-2026-07",
//		Stages: []formula.Stage{
//			{
//				Name: "ngr",
//				Terms: []formula.Term{
//					{Metric: "bets", Coefficient: decimal.FromInt(1)},
//					{Metric: "wins", Coefficient: decimal.FromInt(-1)},
//					{Metric: "bonus_cost", Coefficient: decimal.FromInt(-1)},
//					{Metric: "provider_fees", Coefficient: decimal.FromInt(-1)},
//				},
//			},
//			{
//				Name: "commission_base",
//				Terms: []formula.Term{
//					{Metric: "ngr", Coefficient: decimal.FromInt(1)},
//					{Metric: "carry_in", Coefficient: decimal.FromInt(1)},
//				},
//				Round: &formula.Round{Scale: 2, Mode: decimal.HalfEven},
//				Clamp: &formula.Clamp{Min: new(decimal.Zero)},
//			},
//		},
//	}
//
//	compiled, err := formula.Compile(spec)
//	if err != nil {
//		panic(err)
//	}
//
//	res, err := compiled.Eval(map[string]decimal.Decimal{
//		"bets":          decimal.MustParse("125000.00"),
//		"wins":          decimal.MustParse("118400.50"),
//		"bonus_cost":    decimal.MustParse("2100.00"),
//		"provider_fees": decimal.MustParse("1249.75"),
//		"carry_in":      decimal.MustParse("-4000.00"),
//	})
//	if err != nil {
//		return err
//	}
//	base := res.Final() // "0.00" — NGR 3249.75 + carry -4000 clamps at zero
//	// Persist res (json.Marshal) as the statement explanation; feed base to
//	// tariff for the banded rate, post the outcome via the ledger.
//
// Each stage's value is the exact sum of coefficient × metric, then rounded
// (if Round is set), then clamped (if Clamp is set) — in that order, so the
// final value always honors the bounds. Later stages consume that final
// value.
//
// # Beyond linear — registered functions
//
// A shape staged linear terms can't express is a named Go function; per-deal
// parameters are data closed over at registration, and the Result records the
// function name and the exact args it received:
//
//	capped := func(cap decimal.Decimal) formula.Func {
//		return func(args []decimal.Decimal) (decimal.Decimal, error) {
//			if args[0].Cmp(cap) > 0 {
//				return cap, nil
//			}
//			return args[0], nil
//		}
//	}
//	compiled, err := formula.Compile(spec,
//		formula.WithFunc("cap_monthly", capped(decimal.MustParse("50000"))))
//
// The package is a pure calculator: no storage, no goroutines, no context.
// Tenancy is not a seam here — specs and inputs are caller-supplied values,
// so a multi-tenant app simply loads the tenant's spec. A Compiled is
// immutable and safe for concurrent use.
package formula
