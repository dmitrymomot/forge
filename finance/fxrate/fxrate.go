package fxrate

import (
	"context"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
)

// RateScale is the fractional-digit precision of rates derived by division
// (inverse and cross rates), rounded half-to-even. Direct provider quotes keep
// the scale the provider published. The value is a package constant, not a
// knob, so a recompute from a stored Snapshot always byte-matches.
const RateScale = 12

var one = decimal.FromInt(1)

// Rate quotes one unit of Base in Quote currency: 1 Base = Value Quote, as
// published (or derived from) Provider at AsOf.
type Rate struct {
	// AsOf is the provider's publication time for the underlying snapshot.
	AsOf time.Time
	// Base is the currency being priced, e.g. "EUR".
	Base string
	// Quote is the currency the price is expressed in, e.g. "USD".
	Quote string
	// Provider names the rate source, e.g. "frankfurter".
	Provider string
	// Value is the amount of Quote one Base buys.
	Value decimal.Decimal
}

// Apply converts amount (denominated in Base) to Quote: exact multiply, then
// one rounding to scale fractional digits using mode. The returned Conversion
// carries everything needed to recompute the result byte-for-byte.
func (r Rate) Apply(amount decimal.Decimal, scale int32, mode decimal.RoundingMode) Conversion {
	return Conversion{
		Amount: amount,
		Result: amount.Mul(r.Value).Round(scale, mode),
		Rate:   r,
		Scale:  scale,
		Mode:   mode,
	}
}

// Conversion records a conversion and the exact rate applied — the audit
// answer to "what rate at transaction time". Amount × Rate.Value rounded to
// Scale digits with Mode reproduces Result exactly.
type Conversion struct {
	// Amount is the input, denominated in Rate.Base.
	Amount decimal.Decimal
	// Result is the output, denominated in Rate.Quote.
	Result decimal.Decimal
	// Rate is the rate applied, including provider and as-of time.
	Rate Rate
	// Scale is the fractional-digit count Result was rounded to.
	Scale int32
	// Mode is the rounding mode used.
	Mode decimal.RoundingMode
}

// RateSource fetches a rate snapshot denominated in base. A nil or empty
// quotes slice requests every currency the source offers; otherwise the
// returned snapshot must cover at least the requested quotes. Implementations
// construct the result via NewSnapshot so its invariants hold.
type RateSource interface {
	Fetch(ctx context.Context, base string, quotes []string) (Snapshot, error)
}

// normalizeCode canonicalizes a currency code for lookup: trimmed, uppercase.
func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
