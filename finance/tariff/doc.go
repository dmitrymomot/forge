// Package tariff calculates tiered/banded rates — "25% up to 10, 30% to 50,
// 35% above" — as a pure calculator over decimal and money values.
//
// A Schedule is a validated, immutable band set with one of two rating modes:
// Graduated rates each slice of the base by its own band (tax-bracket
// semantics), Volume rates the entire base by the single band it falls into.
// Apply returns an exact per-band breakdown (Result) in decimal; ApplyMoney
// rates a monetary base with fraction rates and carries the currency through.
// Amounts are exact products — rounding happens once, at settlement, via
// MoneyResult.Round (per-line policy) or Total.Round (per-total policy) with
// an explicit decimal.RoundingMode, so recomputes byte-match. Schedules
// marshal to/from JSON and re-validate on load, so deal band sets live as
// data (e.g. in data/settings) and an edited-invalid set fails to load.
//
// Bands are caller-supplied values: effective-dating is the caller choosing
// which band set applies, and per-tenant or per-deal rating is the caller
// passing that tenant's schedule — there is no lookup inside. Typical
// consumers: usage-billing overage tiers (Apply on the quantity, rates as
// unit prices, wrap with money.New), revenue-share deals and commission
// plans (ApplyMoney, rates as fractions).
//
// What this is NOT: it is not a pricing engine — no effective-dating, no
// proration, no minimums/caps beyond the band shape, and no derivation of the
// base (finance/formula derives bases like NGR or billable usage; tariff
// rates them). Negative (rebate) rates are rejected; corrections post
// forward. Band bounds are plain decimals — ApplyMoney interprets them in the
// base's currency but a Schedule itself is currency-agnostic.
//
// Siblings: decimal (the exact substrate); money (currency-aware amounts and
// settlement rounding); finance/formula (derives the bases tariff rates).
// See docs/packages.md for the package catalog.
//
// # Usage
//
//	d := decimal.MustParse
//	sched, err := tariff.New(tariff.Graduated,
//		tariff.UpTo(d("1000"), d("0.25")),
//		tariff.UpTo(d("5000"), d("0.30")),
//		tariff.Above(d("0.35")),
//	)
//	if err != nil {
//		// invalid band set: unordered bounds, negative rate, ...
//	}
//
//	res, err := sched.ApplyMoney(money.FromMinor(1_000_000, money.USD)) // 10000.00 USD
//	if err != nil {
//		// negative base
//	}
//	// res.Lines: 1000×25% + 4000×30% + 5000×35%, exact
//	settled, err := res.Round(decimal.HalfEven)
//	if err != nil {
//		// unreachable: ApplyMoney results never mix currencies
//	}
//	_ = settled.Total // "3200.00 USD"
package tariff
