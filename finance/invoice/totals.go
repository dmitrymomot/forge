package invoice

import (
	"fmt"
	"slices"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
)

// RoundingPolicy selects where rounding to the currency's minor units
// happens when totals are computed. The requirement is jurisdictional; both
// policies use banker's rounding (decimal.HalfEven), the money package's
// settlement default.
type RoundingPolicy int

const (
	// RoundPerLine rounds each line net and each line's tax individually;
	// totals are exact sums of the rounded parts. This is the default and
	// the common retail/US sales-tax convention.
	RoundPerLine RoundingPolicy = iota
	// RoundPerTotal keeps line math exact, rounds the subtotal and each
	// per-rate tax base once at the document level, and allocates the
	// rounded subtotal back across lines penny-perfect via money.Allocate
	// (largest-remainder), so displayed line nets always sum exactly to the
	// subtotal. This is the common EU VAT convention.
	RoundPerTotal
)

// LineItem is one invoice line. Quantity must be positive, UnitPrice
// non-negative, and TaxRate a non-negative fraction (0.20 for 20%). Tax
// rates are caller-supplied data; the package never determines them.
type LineItem struct {
	// Description labels the line. It is inert data, never validated.
	Description string
	// Quantity multiplies UnitPrice; fractional quantities (1.5 h) are exact
	// decimals.
	Quantity decimal.Decimal
	// UnitPrice is the per-unit net price. Its currency fixes the line
	// currency; all lines of a document must share one currency.
	UnitPrice money.Money
	// TaxRate is the tax fraction applied to the line net.
	TaxRate decimal.Decimal
}

// TaxLine is the per-rate tax summary: every distinct TaxRate across the
// lines produces one TaxLine, zero-rated bases included. TaxLines are sorted
// by ascending rate.
type TaxLine struct {
	Rate   decimal.Decimal
	Base   money.Money
	Amount money.Money
}

// Totals is the computed money summary of a document. All values are rounded
// to the currency's minor units; Subtotal + Tax = Total exactly, and
// LineNets sum exactly to Subtotal.
type Totals struct {
	// Subtotal is the net total before tax.
	Subtotal money.Money
	// TaxLines groups tax by rate, ascending. Under RoundPerTotal the
	// per-rate bases are rounded independently and may differ from Subtotal
	// by a minor unit in aggregate; the tax amounts, not the bases, are the
	// settlement values.
	TaxLines []TaxLine
	// Tax is the sum of all TaxLines amounts.
	Tax money.Money
	// Total is Subtotal + Tax.
	Total money.Money
	// LineNets holds the rounded net per line, index-aligned with the input
	// lines, always summing exactly to Subtotal (allocated under
	// RoundPerTotal).
	LineNets []money.Money
}

// Compute derives Totals from line items under the given rounding policy.
// It validates every line (positive quantity, non-negative price and rate,
// one shared currency) and is a pure calculator, usable standalone for quote
// previews; Issue calls it to freeze the document totals.
func Compute(lines []LineItem, policy RoundingPolicy) (Totals, error) {
	if policy != RoundPerLine && policy != RoundPerTotal {
		return Totals{}, ErrRoundingPolicy
	}
	if len(lines) == 0 {
		return Totals{}, ErrNoLines
	}

	cur := lines[0].UnitPrice.Currency()
	exact := make([]money.Money, len(lines))
	for i, ln := range lines {
		if err := validateLine(i, ln, cur); err != nil {
			return Totals{}, err
		}
		exact[i] = ln.UnitPrice.Mul(ln.Quantity)
	}

	if policy == RoundPerLine {
		return computePerLine(lines, exact, cur)
	}
	return computePerTotal(lines, exact, cur)
}

func validateLine(i int, ln LineItem, cur money.Currency) error {
	switch {
	case !ln.Quantity.IsPositive():
		return fmt.Errorf("%w: line %d: quantity must be positive", ErrInvalidLine, i)
	case ln.UnitPrice.IsNegative():
		return fmt.Errorf("%w: line %d: negative unit price", ErrInvalidLine, i)
	case ln.UnitPrice.Currency().Code == "":
		return fmt.Errorf("%w: line %d: unit price has no currency", ErrInvalidLine, i)
	case ln.TaxRate.IsNegative():
		return fmt.Errorf("%w: line %d: negative tax rate", ErrInvalidLine, i)
	case ln.UnitPrice.Currency().Code != cur.Code:
		return fmt.Errorf("invoice: line %d: %w", i, money.ErrCurrencyMismatch)
	}
	return nil
}

// taxGroup accumulates one distinct rate. base collects rounded line nets
// under RoundPerLine and exact nets under RoundPerTotal.
type taxGroup struct {
	rate   decimal.Decimal
	base   money.Money
	amount money.Money // RoundPerLine only: sum of per-line rounded taxes
}

func computePerLine(lines []LineItem, exact []money.Money, cur money.Currency) (Totals, error) {
	nets := make([]money.Money, len(lines))
	subtotal := money.FromMinor(0, cur)
	var groups []taxGroup
	for i, ln := range lines {
		nets[i] = exact[i].Round(decimal.HalfEven)
		var err error
		if subtotal, err = subtotal.Add(nets[i]); err != nil {
			return Totals{}, err
		}
		tax := nets[i].Mul(ln.TaxRate).Round(decimal.HalfEven)
		g := findGroup(&groups, ln.TaxRate, cur)
		if g.base, err = g.base.Add(nets[i]); err != nil {
			return Totals{}, err
		}
		if g.amount, err = g.amount.Add(tax); err != nil {
			return Totals{}, err
		}
	}
	return finish(subtotal, nets, groups)
}

func computePerTotal(lines []LineItem, exact []money.Money, cur money.Currency) (Totals, error) {
	exactSum := money.FromMinor(0, cur)
	var groups []taxGroup
	for i, ln := range lines {
		var err error
		if exactSum, err = exactSum.Add(exact[i]); err != nil {
			return Totals{}, err
		}
		g := findGroup(&groups, ln.TaxRate, cur)
		if g.base, err = g.base.Add(exact[i]); err != nil {
			return Totals{}, err
		}
	}
	subtotal := exactSum.Round(decimal.HalfEven)

	nets, err := allocateNets(subtotal, exact, cur)
	if err != nil {
		return Totals{}, err
	}
	// Round each rate group once: base and tax derive from the exact group
	// sum, not from per-line rounded values.
	for i := range groups {
		groups[i].amount = groups[i].base.Mul(groups[i].rate).Round(decimal.HalfEven)
		groups[i].base = groups[i].base.Round(decimal.HalfEven)
	}
	return finish(subtotal, nets, groups)
}

// allocateNets distributes the rounded subtotal across lines proportionally
// to their exact nets so the parts sum back exactly. Lines so small that
// every weight truncates to zero minor units fall back to equal weights.
func allocateNets(subtotal money.Money, exact []money.Money, cur money.Currency) ([]money.Money, error) {
	if len(exact) == 1 {
		return []money.Money{subtotal}, nil
	}
	if subtotal.IsZero() {
		nets := make([]money.Money, len(exact))
		for i := range nets {
			nets[i] = money.FromMinor(0, cur)
		}
		return nets, nil
	}
	weights := make([]int, len(exact))
	anyPositive := false
	for i, e := range exact {
		w, ok := e.MinorOK()
		if !ok {
			return nil, fmt.Errorf("invoice: line %d: %w", i, money.ErrOverflow)
		}
		weights[i] = int(w)
		anyPositive = anyPositive || w > 0
	}
	if !anyPositive {
		for i := range weights {
			weights[i] = 1
		}
	}
	return subtotal.Allocate(weights...)
}

func findGroup(groups *[]taxGroup, rate decimal.Decimal, cur money.Currency) *taxGroup {
	for i := range *groups {
		if (*groups)[i].rate.Cmp(rate) == 0 {
			return &(*groups)[i]
		}
	}
	zero := money.FromMinor(0, cur)
	*groups = append(*groups, taxGroup{rate: rate, base: zero, amount: zero})
	return &(*groups)[len(*groups)-1]
}

func finish(subtotal money.Money, nets []money.Money, groups []taxGroup) (Totals, error) {
	slices.SortFunc(groups, func(a, b taxGroup) int { return a.rate.Cmp(b.rate) })
	taxLines := make([]TaxLine, len(groups))
	tax := money.FromMinor(0, subtotal.Currency())
	var err error
	for i, g := range groups {
		taxLines[i] = TaxLine{Rate: g.rate, Base: g.base, Amount: g.amount}
		if tax, err = tax.Add(g.amount); err != nil {
			return Totals{}, err
		}
	}
	total, err := subtotal.Add(tax)
	if err != nil {
		return Totals{}, err
	}
	return Totals{Subtotal: subtotal, TaxLines: taxLines, Tax: tax, Total: total, LineNets: nets}, nil
}
