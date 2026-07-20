package invoice_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/finance/invoice"
)

func line(t *testing.T, qty, unit, rate string, c money.Currency) invoice.LineItem {
	t.Helper()
	return invoice.LineItem{
		Description: "test line",
		Quantity:    decimal.MustParse(qty),
		UnitPrice:   money.New(decimal.MustParse(unit), c),
		TaxRate:     decimal.MustParse(rate),
	}
}

func assertMoney(t *testing.T, want string, got money.Money) {
	t.Helper()
	w, err := money.Parse(want, got.Currency())
	require.NoError(t, err)
	eq, err := w.Equal(got)
	require.NoError(t, err)
	assert.True(t, eq, "want %s, got %s", want, got)
}

func TestCompute_PerLineVsPerTotal(t *testing.T) {
	t.Parallel()

	// The classic VAT divergence: 3 × 33.33 at 20%.
	lines := []invoice.LineItem{
		line(t, "1", "33.33", "0.20", money.EUR),
		line(t, "1", "33.33", "0.20", money.EUR),
		line(t, "1", "33.33", "0.20", money.EUR),
	}

	perLine, err := invoice.Compute(lines, invoice.RoundPerLine)
	require.NoError(t, err)
	assertMoney(t, "99.99", perLine.Subtotal)
	assertMoney(t, "20.01", perLine.Tax) // 3 × round(6.666) = 3 × 6.67
	assertMoney(t, "120.00", perLine.Total)
	require.Len(t, perLine.TaxLines, 1)
	assertMoney(t, "99.99", perLine.TaxLines[0].Base)
	assertMoney(t, "20.01", perLine.TaxLines[0].Amount)

	perTotal, err := invoice.Compute(lines, invoice.RoundPerTotal)
	require.NoError(t, err)
	assertMoney(t, "99.99", perTotal.Subtotal)
	assertMoney(t, "20.00", perTotal.Tax) // round(99.99 × 0.20) = round(19.998)
	assertMoney(t, "119.99", perTotal.Total)
}

func TestCompute_PerTotalAllocatesLineNets(t *testing.T) {
	t.Parallel()

	// Each line rounds down individually (0.125 → 0.12, banker's), but the
	// exact sum 0.25 must come back penny-perfect across the line nets.
	lines := []invoice.LineItem{
		line(t, "1", "0.125", "0", money.USD),
		line(t, "1", "0.125", "0", money.USD),
	}

	perLine, err := invoice.Compute(lines, invoice.RoundPerLine)
	require.NoError(t, err)
	assertMoney(t, "0.24", perLine.Subtotal)
	assertMoney(t, "0.12", perLine.LineNets[0])
	assertMoney(t, "0.12", perLine.LineNets[1])

	perTotal, err := invoice.Compute(lines, invoice.RoundPerTotal)
	require.NoError(t, err)
	assertMoney(t, "0.25", perTotal.Subtotal)
	assertMoney(t, "0.13", perTotal.LineNets[0])
	assertMoney(t, "0.12", perTotal.LineNets[1])
}

func TestCompute_TaxGroupingSortedByRate(t *testing.T) {
	t.Parallel()

	lines := []invoice.LineItem{
		line(t, "1", "10.00", "0.20", money.EUR),
		line(t, "1", "20.00", "0", money.EUR),
		line(t, "1", "30.00", "0.07", money.EUR),
		line(t, "2", "5.00", "0.20", money.EUR),
	}
	totals, err := invoice.Compute(lines, invoice.RoundPerLine)
	require.NoError(t, err)

	require.Len(t, totals.TaxLines, 3)
	assert.Equal(t, 0, totals.TaxLines[0].Rate.Cmp(decimal.Zero))
	assert.Equal(t, 0, totals.TaxLines[1].Rate.Cmp(decimal.MustParse("0.07")))
	assert.Equal(t, 0, totals.TaxLines[2].Rate.Cmp(decimal.MustParse("0.20")))

	// Zero-rated base is reported, not dropped.
	assertMoney(t, "20.00", totals.TaxLines[0].Base)
	assertMoney(t, "0.00", totals.TaxLines[0].Amount)
	assertMoney(t, "30.00", totals.TaxLines[1].Base)
	assertMoney(t, "2.10", totals.TaxLines[1].Amount)
	assertMoney(t, "20.00", totals.TaxLines[2].Base)
	assertMoney(t, "4.00", totals.TaxLines[2].Amount)
	assertMoney(t, "70.00", totals.Subtotal)
	assertMoney(t, "6.10", totals.Tax)
	assertMoney(t, "76.10", totals.Total)
}

func TestCompute_ZeroMinorUnitCurrency(t *testing.T) {
	t.Parallel()

	lines := []invoice.LineItem{
		line(t, "1", "100.5", "0.10", money.JPY),
		line(t, "1", "100.5", "0.10", money.JPY),
	}

	perLine, err := invoice.Compute(lines, invoice.RoundPerLine)
	require.NoError(t, err)
	assertMoney(t, "200", perLine.Subtotal) // 100.5 → 100 each (banker's)
	assertMoney(t, "20", perLine.Tax)

	perTotal, err := invoice.Compute(lines, invoice.RoundPerTotal)
	require.NoError(t, err)
	assertMoney(t, "201", perTotal.Subtotal)
	assertMoney(t, "20", perTotal.Tax) // round(201 × 0.10) = round(20.1)
	assertMoney(t, "221", perTotal.Total)
}

func TestCompute_ZeroTotal(t *testing.T) {
	t.Parallel()

	lines := []invoice.LineItem{line(t, "1", "0.00", "0.20", money.USD)}
	for _, policy := range []invoice.RoundingPolicy{invoice.RoundPerLine, invoice.RoundPerTotal} {
		totals, err := invoice.Compute(lines, policy)
		require.NoError(t, err)
		assertMoney(t, "0.00", totals.Subtotal)
		assertMoney(t, "0.00", totals.Total)
		require.Len(t, totals.LineNets, 1)
		assertMoney(t, "0.00", totals.LineNets[0])
	}
}

func TestCompute_SubMinorLinesFallBackToEqualWeights(t *testing.T) {
	t.Parallel()

	// Every line net truncates to zero minor units, yet the exact sum does
	// not — allocation falls back to equal weights instead of failing.
	lines := []invoice.LineItem{
		line(t, "1", "0.004", "0", money.USD),
		line(t, "1", "0.004", "0", money.USD),
		line(t, "1", "0.004", "0", money.USD),
	}
	totals, err := invoice.Compute(lines, invoice.RoundPerTotal)
	require.NoError(t, err)
	assertMoney(t, "0.01", totals.Subtotal)
	sum, err := money.Sum(totals.LineNets[0], totals.LineNets[1:]...)
	require.NoError(t, err)
	assertMoney(t, "0.01", sum)
}

func TestCompute_Errors(t *testing.T) {
	t.Parallel()

	valid := line(t, "1", "10.00", "0.20", money.EUR)

	t.Run("no lines", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.Compute(nil, invoice.RoundPerLine)
		assert.ErrorIs(t, err, invoice.ErrNoLines)
	})
	t.Run("unknown policy", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.Compute([]invoice.LineItem{valid}, invoice.RoundingPolicy(99))
		assert.ErrorIs(t, err, invoice.ErrRoundingPolicy)
	})
	t.Run("zero quantity", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.Compute([]invoice.LineItem{line(t, "0", "10.00", "0.20", money.EUR)}, invoice.RoundPerLine)
		assert.ErrorIs(t, err, invoice.ErrInvalidLine)
	})
	t.Run("negative quantity", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.Compute([]invoice.LineItem{line(t, "-1", "10.00", "0.20", money.EUR)}, invoice.RoundPerLine)
		assert.ErrorIs(t, err, invoice.ErrInvalidLine)
	})
	t.Run("negative unit price", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.Compute([]invoice.LineItem{line(t, "1", "-10.00", "0.20", money.EUR)}, invoice.RoundPerLine)
		assert.ErrorIs(t, err, invoice.ErrInvalidLine)
	})
	t.Run("negative tax rate", func(t *testing.T) {
		t.Parallel()
		_, err := invoice.Compute([]invoice.LineItem{line(t, "1", "10.00", "-0.20", money.EUR)}, invoice.RoundPerLine)
		assert.ErrorIs(t, err, invoice.ErrInvalidLine)
	})
	t.Run("no currency", func(t *testing.T) {
		t.Parallel()
		bad := invoice.LineItem{Quantity: decimal.FromInt(1), TaxRate: decimal.Zero}
		_, err := invoice.Compute([]invoice.LineItem{bad}, invoice.RoundPerLine)
		assert.ErrorIs(t, err, invoice.ErrInvalidLine)
	})
	t.Run("mixed currencies", func(t *testing.T) {
		t.Parallel()
		mixed := []invoice.LineItem{valid, line(t, "1", "10.00", "0.20", money.USD)}
		_, err := invoice.Compute(mixed, invoice.RoundPerLine)
		assert.ErrorIs(t, err, money.ErrCurrencyMismatch)
	})
}

// TestCompute_Invariants fuzzes deterministic pseudo-random documents and
// asserts the structural money invariants both policies promise.
func TestCompute_Invariants(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(20260721))
	rates := []string{"0", "0.05", "0.07", "0.19", "0.20", "0.21"}
	quantities := []string{"1", "2", "3", "0.5", "1.5", "12"}

	for range 200 {
		n := 1 + rng.Intn(8)
		lines := make([]invoice.LineItem, n)
		for i := range lines {
			lines[i] = line(t,
				quantities[rng.Intn(len(quantities))],
				decimal.New(int64(rng.Intn(1_000_000)), 2).String(),
				rates[rng.Intn(len(rates))],
				money.EUR,
			)
		}
		for _, policy := range []invoice.RoundingPolicy{invoice.RoundPerLine, invoice.RoundPerTotal} {
			totals, err := invoice.Compute(lines, policy)
			require.NoError(t, err)
			require.Len(t, totals.LineNets, n)

			// Line nets sum exactly to the subtotal.
			netSum, err := money.Sum(totals.LineNets[0], totals.LineNets[1:]...)
			require.NoError(t, err)
			eq, err := netSum.Equal(totals.Subtotal)
			require.NoError(t, err)
			require.True(t, eq, "line nets %v != subtotal %s", totals.LineNets, totals.Subtotal)

			// Subtotal + tax = total, and tax is the sum of tax lines.
			wantTotal, err := totals.Subtotal.Add(totals.Tax)
			require.NoError(t, err)
			eq, err = wantTotal.Equal(totals.Total)
			require.NoError(t, err)
			require.True(t, eq)

			taxSum := money.FromMinor(0, money.EUR)
			for i, tl := range totals.TaxLines {
				taxSum, err = taxSum.Add(tl.Amount)
				require.NoError(t, err)
				if i > 0 {
					require.Positive(t, tl.Rate.Cmp(totals.TaxLines[i-1].Rate), "tax lines not sorted")
				}
			}
			eq, err = taxSum.Equal(totals.Tax)
			require.NoError(t, err)
			require.True(t, eq)
		}
	}
}
