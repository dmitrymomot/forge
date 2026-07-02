package money_test

import (
	"testing"

	"github.com/dmitrymomot/forge/decimal"
	"github.com/dmitrymomot/forge/money"
)

func BenchmarkNew(b *testing.B) {
	amount := decimal.New(150, 2) // 1.50
	b.ReportAllocs()
	for b.Loop() {
		_ = money.New(amount, money.USD)
	}
}

func BenchmarkFromMinor(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = money.FromMinor(150, money.USD)
	}
}

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = money.Parse("1234.56", money.USD)
	}
}

func BenchmarkAmount(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.Amount()
	}
}

func BenchmarkMinor(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.Minor()
	}
}

func BenchmarkString(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.String()
	}
}

func BenchmarkAdd(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	n := money.FromMinor(275, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Add(n)
	}
}

func BenchmarkSub(b *testing.B) {
	m := money.FromMinor(500, money.USD)
	n := money.FromMinor(275, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Sub(n)
	}
}

func BenchmarkMul(b *testing.B) {
	m := money.FromMinor(1000, money.USD)
	factor := decimal.New(825, 3) // 0.825 tax-like rate
	b.ReportAllocs()
	for b.Loop() {
		_ = m.Mul(factor)
	}
}

func BenchmarkRound(b *testing.B) {
	m, _ := money.Parse("1.23456", money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.Round(decimal.HalfEven)
	}
}

func BenchmarkCmp(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	n := money.FromMinor(275, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Cmp(n)
	}
}

func BenchmarkAllocate(b *testing.B) {
	m := money.FromMinor(1000, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Allocate(1, 2, 3)
	}
}

func BenchmarkSplit(b *testing.B) {
	m := money.FromMinor(1000, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Split(3)
	}
}

func BenchmarkCurrencyByCode(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = money.CurrencyByCode("usd")
	}
}

func BenchmarkNeg(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_ = m.Neg()
	}
}

func BenchmarkEqual(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	n := money.FromMinor(150, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Equal(n)
	}
}

func BenchmarkSum(b *testing.B) {
	x := money.FromMinor(150, money.USD)
	y := money.FromMinor(275, money.USD)
	z := money.FromMinor(75, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = money.Sum(x, y, z)
	}
}

func BenchmarkMoneyMarshalJSON(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.MarshalJSON()
	}
}

func BenchmarkMoneyValue(b *testing.B) {
	m := money.FromMinor(150, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Value()
	}
}

func BenchmarkMoneyScan(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var m money.Money
		_ = m.Scan("1.50 USD")
	}
}
