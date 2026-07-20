package tariff_test

import (
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/finance/tariff"
)

func TestApplyMoneyGraduated(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Graduated)

	res, err := s.ApplyMoney(money.FromMinor(10000, money.USD)) // 100.00 USD
	if err != nil {
		t.Fatalf("ApplyMoney: %v", err)
	}
	if got := res.Total.Currency().Code; got != "USD" {
		t.Fatalf("Total currency = %q, want USD", got)
	}
	if res.Total.Amount().Cmp(d("32")) != 0 {
		t.Fatalf("Total = %s, want 32", res.Total.Amount())
	}
	if len(res.Lines) != 3 {
		t.Fatalf("len(Lines) = %d, want 3", len(res.Lines))
	}
	for i, l := range res.Lines {
		if l.Slice.Currency().Code != "USD" || l.Amount.Currency().Code != "USD" {
			t.Fatalf("line %d currency not propagated: slice %s, amount %s", i, l.Slice, l.Amount)
		}
	}
	if res.Lines[2].Amount.Amount().Cmp(d("17.5")) != 0 {
		t.Fatalf("open-band amount = %s, want 17.5", res.Lines[2].Amount.Amount())
	}
}

func TestApplyMoneyZeroAndNegative(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Volume)

	res, err := s.ApplyMoney(money.FromMinor(0, money.EUR))
	if err != nil {
		t.Fatalf("ApplyMoney(0): %v", err)
	}
	if len(res.Lines) != 0 || !res.Total.IsZero() {
		t.Fatalf("zero base: lines %d total %s", len(res.Lines), res.Total)
	}
	// The zero total still carries the base's currency.
	if got := res.Total.Currency().Code; got != "EUR" {
		t.Fatalf("zero-base Total currency = %q, want EUR", got)
	}

	if _, err := s.ApplyMoney(money.FromMinor(-1, money.EUR)); !errors.Is(err, tariff.ErrNegativeBase) {
		t.Fatalf("negative base = %v, want ErrNegativeBase", err)
	}

	var zero tariff.Schedule
	if _, err := zero.ApplyMoney(money.FromMinor(100, money.USD)); !errors.Is(err, tariff.ErrNoBands) {
		t.Fatalf("zero Schedule = %v, want ErrNoBands", err)
	}
}

func TestMoneyResultRound(t *testing.T) {
	t.Parallel()
	// 25.75% up to 10, 33.3% above — sub-cent amounts on both lines.
	s, err := tariff.New(tariff.Graduated,
		tariff.UpTo(d("10"), d("0.2575")),
		tariff.Above(d("0.333")),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := s.ApplyMoney(money.New(d("10.05"), money.USD))
	if err != nil {
		t.Fatalf("ApplyMoney: %v", err)
	}
	// Exact: 10×0.2575 = 2.575; 0.05×0.333 = 0.01665; total 2.59165.
	if res.Total.Amount().Cmp(d("2.59165")) != 0 {
		t.Fatalf("exact Total = %s, want 2.59165", res.Total.Amount())
	}

	settled, err := res.Round(decimal.HalfEven)
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	// Per-line: 2.575 → 2.58 (half-even to even cent), 0.01665 → 0.02; total 2.60.
	if got := settled.Lines[0].Amount.Amount(); got.Cmp(d("2.58")) != 0 {
		t.Fatalf("line 0 rounded = %s, want 2.58", got)
	}
	if got := settled.Lines[1].Amount.Amount(); got.Cmp(d("0.02")) != 0 {
		t.Fatalf("line 1 rounded = %s, want 0.02", got)
	}
	if got := settled.Total.Amount(); got.Cmp(d("2.60")) != 0 {
		t.Fatalf("per-line Total = %s, want 2.60", got)
	}
	// Per-total policy differs: 2.59165 → 2.59.
	if got := res.Total.Round(decimal.HalfEven).Amount(); got.Cmp(d("2.59")) != 0 {
		t.Fatalf("per-total = %s, want 2.59", got)
	}
	// Rounding must not mutate the exact result.
	if res.Lines[0].Amount.Amount().Cmp(d("2.575")) != 0 {
		t.Fatalf("Round mutated the exact result: %s", res.Lines[0].Amount.Amount())
	}
}

func TestMoneyResultRoundEmpty(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Graduated)
	res, err := s.ApplyMoney(money.FromMinor(0, money.USD))
	if err != nil {
		t.Fatalf("ApplyMoney: %v", err)
	}
	settled, err := res.Round(decimal.HalfUp)
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if len(settled.Lines) != 0 || !settled.Total.IsZero() {
		t.Fatalf("empty round: lines %d total %s", len(settled.Lines), settled.Total)
	}
	if got := settled.Total.Currency().Code; got != "USD" {
		t.Fatalf("empty round Total currency = %q, want USD", got)
	}
}

func TestMoneyResultRoundMixedCurrencies(t *testing.T) {
	t.Parallel()
	// A hand-assembled result mixing currencies must fail closed, not sum.
	mixed := tariff.MoneyResult{Lines: []tariff.MoneyLine{
		{Amount: money.FromMinor(100, money.USD)},
		{Amount: money.FromMinor(100, money.EUR)},
	}}
	if _, err := mixed.Round(decimal.HalfEven); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Fatalf("mixed Round = %v, want ErrCurrencyMismatch", err)
	}
}

func TestApplyMoneyJPYSettlement(t *testing.T) {
	t.Parallel()
	// Zero-minor-unit currency: settlement rounds to whole yen.
	s, err := tariff.New(tariff.Volume, tariff.Above(d("0.155")))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := s.ApplyMoney(money.FromMinor(999, money.JPY))
	if err != nil {
		t.Fatalf("ApplyMoney: %v", err)
	}
	if res.Total.Amount().Cmp(d("154.845")) != 0 {
		t.Fatalf("exact Total = %s, want 154.845", res.Total.Amount())
	}
	settled, err := res.Round(decimal.HalfEven)
	if err != nil {
		t.Fatalf("Round: %v", err)
	}
	if got := settled.Total.Amount(); got.Cmp(d("155")) != 0 {
		t.Fatalf("settled Total = %s, want 155", got)
	}
}
