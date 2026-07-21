package tariff_test

import (
	"strconv"
	"testing"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/finance/tariff"
)

func benchSchedule(b *testing.B, mode tariff.Mode, n int) tariff.Schedule {
	b.Helper()
	bands := make([]tariff.Band, 0, n)
	for i := range n - 1 {
		bound := decimal.FromInt(int64((i + 1) * 100))
		rate := decimal.New(int64(20+i), 2)
		bands = append(bands, tariff.UpTo(bound, rate))
	}
	bands = append(bands, tariff.Above(decimal.New(int64(20+n), 2)))
	s, err := tariff.New(mode, bands...)
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	return s
}

func BenchmarkApplyGraduated(b *testing.B) {
	for _, n := range []int{3, 10} {
		b.Run(strconv.Itoa(n)+"bands", func(b *testing.B) {
			s := benchSchedule(b, tariff.Graduated, n)
			base := decimal.FromInt(int64(n * 100)) // touches every band
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.Apply(base); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkApplyVolume(b *testing.B) {
	s := benchSchedule(b, tariff.Volume, 10)
	base := decimal.MustParse("450.50") // mid-schedule band
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Apply(base); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApplyMoney(b *testing.B) {
	s := benchSchedule(b, tariff.Graduated, 3)
	base := money.FromMinor(100_000, money.USD)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.ApplyMoney(base); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMoneyResultRound(b *testing.B) {
	s := benchSchedule(b, tariff.Graduated, 3)
	res, err := s.ApplyMoney(money.New(decimal.MustParse("1000.05"), money.USD))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := res.Round(decimal.HalfEven); err != nil {
			b.Fatal(err)
		}
	}
}
