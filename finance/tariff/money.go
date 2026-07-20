package tariff

import (
	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
)

// MoneyLine is one band's contribution to a MoneyResult.
type MoneyLine struct {
	// Slice is the portion of the base rated by this band.
	Slice money.Money
	// Amount is Slice × Rate, exact (never rounded) — round at settlement.
	Amount money.Money
	// Rate is the band's fraction, echoed for statement rendering.
	Rate decimal.Decimal
	// Band is the index of the band in the schedule.
	Band int
}

// MoneyResult is the exact outcome of rating a monetary base. All amounts
// carry the base's currency.
type MoneyResult struct {
	// Lines holds one entry per band that rated a non-empty slice, in band
	// order. A zero base produces no lines.
	Lines []MoneyLine
	// Total is the exact sum of the line amounts.
	Total money.Money
}

// ApplyMoney rates a monetary base — a revenue-share or commission
// calculation. Band bounds are interpreted as amounts in the base's currency
// and rates as fractions (0.25 = 25%). Amounts are exact; call Round (per-line
// policy) or Total.Round (per-total policy) at settlement. For usage pricing
// — quantity bands with per-unit money rates — use Apply on the quantity and
// wrap the result with money.New.
func (s Schedule) ApplyMoney(base money.Money) (MoneyResult, error) {
	r, err := s.Apply(base.Amount())
	if err != nil {
		return MoneyResult{}, err
	}
	cur := base.Currency()
	var lines []MoneyLine
	if len(r.Lines) > 0 {
		lines = make([]MoneyLine, len(r.Lines))
		for i, l := range r.Lines {
			lines[i] = MoneyLine{
				Band:   l.Band,
				Rate:   l.Rate,
				Slice:  money.New(l.Slice, cur),
				Amount: money.New(l.Amount, cur),
			}
		}
	}
	return MoneyResult{Lines: lines, Total: money.New(r.Total, cur)}, nil
}

// Round returns a copy with every line amount rounded to its currency's minor
// units using mode, and Total recomputed as the sum of the rounded lines —
// the per-line rounding policy, where statement lines are settled amounts and
// the total is their exact sum. For the per-total policy round the total
// alone: r.Total.Round(mode). It returns money.ErrCurrencyMismatch only for a
// hand-assembled result whose lines mix currencies; results from ApplyMoney
// never do.
func (r MoneyResult) Round(mode decimal.RoundingMode) (MoneyResult, error) {
	if len(r.Lines) == 0 {
		return MoneyResult{Total: r.Total.Round(mode)}, nil
	}
	lines := make([]MoneyLine, len(r.Lines))
	total := money.New(decimal.Zero, r.Lines[0].Amount.Currency())
	for i, l := range r.Lines {
		l.Amount = l.Amount.Round(mode)
		lines[i] = l
		var err error
		if total, err = total.Add(l.Amount); err != nil {
			return MoneyResult{}, err
		}
	}
	return MoneyResult{Lines: lines, Total: total}, nil
}
