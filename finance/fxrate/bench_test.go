package fxrate_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/fxrate"
)

func benchSnapshot(b *testing.B) fxrate.Snapshot {
	b.Helper()
	rates := map[string]decimal.Decimal{
		"USD": decimal.MustParse("1.0850"),
		"GBP": decimal.MustParse("0.8425"),
		"JPY": decimal.MustParse("160.23"),
		"CHF": decimal.MustParse("0.9782"),
	}
	// Pad to a realistic full-table size.
	for i := range 26 {
		rates["X"+strconv.Itoa(i)+"A"] = decimal.MustParse("1.2345")
	}
	s, err := fxrate.NewSnapshot("EUR", "bench", time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC), rates)
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func BenchmarkSnapshotConvertDirect(b *testing.B) {
	s := benchSnapshot(b)
	amount := decimal.MustParse("99.90")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Convert(amount, "EUR", "USD", 2, decimal.HalfEven); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSnapshotConvertCross(b *testing.B) {
	s := benchSnapshot(b)
	amount := decimal.MustParse("99.90")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Convert(amount, "USD", "GBP", 2, decimal.HalfEven); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSnapshotRateCross(b *testing.B) {
	s := benchSnapshot(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Rate("JPY", "CHF"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConverterConvertCached(b *testing.B) {
	src, err := fxrate.NewStaticSource(benchSnapshot(b))
	if err != nil {
		b.Fatal(err)
	}
	conv, err := fxrate.New(src, "EUR", fxrate.WithTTL(24*time.Hour))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	amount := decimal.MustParse("99.90")
	if _, err := conv.Snapshot(ctx); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := conv.Convert(ctx, amount, "EUR", "USD", 2, decimal.HalfEven); err != nil {
			b.Fatal(err)
		}
	}
}
