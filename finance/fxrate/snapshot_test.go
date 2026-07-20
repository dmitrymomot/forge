package fxrate_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/fxrate"
)

var asOf = time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC)

func mustSnapshot(t *testing.T, base string, rates map[string]decimal.Decimal) fxrate.Snapshot {
	t.Helper()
	s, err := fxrate.NewSnapshot(base, "test", asOf, rates)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	return s
}

func d(s string) decimal.Decimal { return decimal.MustParse(s) }

func TestNewSnapshotValidation(t *testing.T) {
	t.Parallel()

	valid := map[string]decimal.Decimal{"USD": d("1.0850")}
	tests := []struct {
		name     string
		base     string
		provider string
		asOf     time.Time
		rates    map[string]decimal.Decimal
		wantErr  error
	}{
		{"empty base", "", "test", asOf, valid, fxrate.ErrInvalidSnapshot},
		{"blank base", "   ", "test", asOf, valid, fxrate.ErrInvalidSnapshot},
		{"empty provider", "EUR", "", asOf, valid, fxrate.ErrInvalidSnapshot},
		{"zero asOf", "EUR", "test", time.Time{}, valid, fxrate.ErrInvalidSnapshot},
		{"nil rates", "EUR", "test", asOf, nil, fxrate.ErrInvalidSnapshot},
		{"empty rates", "EUR", "test", asOf, map[string]decimal.Decimal{}, fxrate.ErrInvalidSnapshot},
		{"empty code", "EUR", "test", asOf, map[string]decimal.Decimal{"  ": d("1")}, fxrate.ErrInvalidSnapshot},
		{"zero rate", "EUR", "test", asOf, map[string]decimal.Decimal{"USD": decimal.Zero}, fxrate.ErrInvalidRate},
		{"negative rate", "EUR", "test", asOf, map[string]decimal.Decimal{"USD": d("-1.08")}, fxrate.ErrInvalidRate},
		{"base self-rate not one", "EUR", "test", asOf, map[string]decimal.Decimal{"EUR": d("1.1"), "USD": d("1.0850")}, fxrate.ErrInvalidRate},
		{"only base self-rate", "EUR", "test", asOf, map[string]decimal.Decimal{"EUR": d("1")}, fxrate.ErrInvalidSnapshot},
		{"duplicate after normalization", "EUR", "test", asOf, map[string]decimal.Decimal{"USD": d("1.08"), " usd": d("1.09")}, fxrate.ErrInvalidSnapshot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := fxrate.NewSnapshot(tt.base, tt.provider, tt.asOf, tt.rates)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewSnapshotNormalizes(t *testing.T) {
	t.Parallel()

	s := mustSnapshot(t, " eur ", map[string]decimal.Decimal{" usd ": d("1.0850"), "eur": d("1.00")})
	if s.Base() != "EUR" {
		t.Fatalf("Base = %q, want EUR", s.Base())
	}
	for _, code := range []string{"USD", "usd", " Usd "} {
		if !s.Has(code) {
			t.Fatalf("Has(%q) = false, want true", code)
		}
	}
	if !s.Has("EUR") {
		t.Fatal("Has(base) = false, want true")
	}
	if s.Has("GBP") {
		t.Fatal("Has(GBP) = true, want false")
	}
}

func TestSnapshotAccessors(t *testing.T) {
	t.Parallel()

	s := mustSnapshot(t, "EUR", map[string]decimal.Decimal{"USD": d("1.0850"), "GBP": d("0.8425")})
	if s.IsZero() {
		t.Fatal("IsZero = true for constructed snapshot")
	}
	if s.Provider() != "test" {
		t.Fatalf("Provider = %q", s.Provider())
	}
	if !s.AsOf().Equal(asOf) {
		t.Fatalf("AsOf = %v", s.AsOf())
	}
	want := []string{"EUR", "GBP", "USD"}
	got := s.Currencies()
	if len(got) != len(want) {
		t.Fatalf("Currencies = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Currencies = %v, want %v", got, want)
		}
	}
}

func TestZeroSnapshot(t *testing.T) {
	t.Parallel()

	var s fxrate.Snapshot
	if !s.IsZero() {
		t.Fatal("IsZero = false for zero value")
	}
	if s.Has("USD") {
		t.Fatal("zero snapshot Has = true")
	}
	if s.Currencies() != nil {
		t.Fatal("zero snapshot Currencies != nil")
	}
	if _, err := s.Rate("USD", "EUR"); !errors.Is(err, fxrate.ErrUnknownCurrency) {
		t.Fatalf("Rate on zero snapshot: %v", err)
	}
}

func TestSnapshotRate(t *testing.T) {
	t.Parallel()

	s := mustSnapshot(t, "EUR", map[string]decimal.Decimal{
		"USD": d("2"),
		"GBP": d("0.5"),
		"JPY": d("3"),
	})

	tests := []struct {
		name     string
		from, to string
		want     decimal.Decimal
	}{
		{"identity", "USD", "USD", d("1")},
		{"identity base", "EUR", "EUR", d("1")},
		{"direct keeps provider scale", "EUR", "USD", d("2")},
		{"inverse", "USD", "EUR", d("0.5")},
		{"inverse rounds half-even", "JPY", "EUR", d("0.333333333333")},
		{"cross", "USD", "GBP", d("0.25")},
		{"cross other way", "GBP", "USD", d("4")},
		{"case-insensitive lookup", "usd", "gbp", d("0.25")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := s.Rate(tt.from, tt.to)
			if err != nil {
				t.Fatalf("Rate: %v", err)
			}
			if !r.Value.Equal(tt.want) {
				t.Fatalf("Rate(%s→%s) = %s, want %s", tt.from, tt.to, r.Value, tt.want)
			}
			if r.Provider != "test" || !r.AsOf.Equal(asOf) {
				t.Fatalf("rate metadata not carried: %+v", r)
			}
			if r.Base != "USD" && r.Base != "EUR" && r.Base != "JPY" && r.Base != "GBP" {
				t.Fatalf("Base not normalized: %q", r.Base)
			}
		})
	}

	if _, err := s.Rate("XXX", "USD"); !errors.Is(err, fxrate.ErrUnknownCurrency) {
		t.Fatalf("unknown from: %v", err)
	}
	if _, err := s.Rate("USD", "XXX"); !errors.Is(err, fxrate.ErrUnknownCurrency) {
		t.Fatalf("unknown to: %v", err)
	}
}

func TestSnapshotConvert(t *testing.T) {
	t.Parallel()

	s := mustSnapshot(t, "EUR", map[string]decimal.Decimal{"USD": d("1.0850"), "GBP": d("0.8425")})

	c, err := s.Convert(d("100.00"), "EUR", "USD", 2, decimal.HalfEven)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got := c.Result.String(); got != "108.50" {
		t.Fatalf("Result = %s, want 108.50", got)
	}
	if c.Rate.Base != "EUR" || c.Rate.Quote != "USD" || c.Scale != 2 || c.Mode != decimal.HalfEven {
		t.Fatalf("conversion record incomplete: %+v", c)
	}

	// The record alone must reproduce the result: exact multiply, one round.
	recomputed := c.Amount.Mul(c.Rate.Value).Round(c.Scale, c.Mode)
	if recomputed.String() != c.Result.String() {
		t.Fatalf("recompute = %s, want %s", recomputed, c.Result)
	}

	if _, err := s.Convert(d("1"), "EUR", "XXX", 2, decimal.HalfEven); !errors.Is(err, fxrate.ErrUnknownCurrency) {
		t.Fatalf("unknown currency: %v", err)
	}

	// Negative amounts (refunds) convert like positives: -50.00 × 0.8425 =
	// -42.125, a tie whose even neighbor is -42.12.
	c, err = s.Convert(d("-50.00"), "EUR", "GBP", 2, decimal.HalfEven)
	if err != nil {
		t.Fatalf("Convert negative: %v", err)
	}
	if got := c.Result.String(); got != "-42.12" {
		t.Fatalf("Result = %s, want -42.12", got)
	}
}

func TestSnapshotImmutableFromCallerMap(t *testing.T) {
	t.Parallel()

	rates := map[string]decimal.Decimal{"USD": d("2")}
	s := mustSnapshot(t, "EUR", rates)
	rates["USD"] = d("999")
	rates["GBP"] = d("1")

	r, err := s.Rate("EUR", "USD")
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	if !r.Value.Equal(d("2")) {
		t.Fatalf("snapshot mutated through caller map: %s", r.Value)
	}
	if s.Has("GBP") {
		t.Fatal("snapshot grew through caller map")
	}
}

func TestSnapshotJSONRoundTrip(t *testing.T) {
	t.Parallel()

	s := mustSnapshot(t, "EUR", map[string]decimal.Decimal{"USD": d("1.0850"), "GBP": d("0.8425")})
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var back fxrate.Snapshot
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.Base() != s.Base() || back.Provider() != s.Provider() || !back.AsOf().Equal(s.AsOf()) {
		t.Fatalf("metadata drifted: %+v", back)
	}

	// A conversion recomputed from the stored snapshot byte-matches.
	orig, err := s.Convert(d("99.90"), "USD", "GBP", 2, decimal.HalfEven)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	replay, err := back.Convert(d("99.90"), "USD", "GBP", 2, decimal.HalfEven)
	if err != nil {
		t.Fatalf("replay Convert: %v", err)
	}
	if orig.Result.String() != replay.Result.String() || orig.Rate.Value.String() != replay.Rate.Value.String() {
		t.Fatalf("replay drifted: %s@%s vs %s@%s", orig.Result, orig.Rate.Value, replay.Result, replay.Rate.Value)
	}
}

func TestSnapshotUnmarshalFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"negative rate", `{"as_of":"2026-07-20T16:00:00Z","base":"EUR","provider":"test","rates":{"USD":"-1"}}`, fxrate.ErrInvalidRate},
		{"missing base", `{"as_of":"2026-07-20T16:00:00Z","provider":"test","rates":{"USD":"1.08"}}`, fxrate.ErrInvalidSnapshot},
		{"no rates", `{"as_of":"2026-07-20T16:00:00Z","base":"EUR","provider":"test","rates":{}}`, fxrate.ErrInvalidSnapshot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var s fxrate.Snapshot
			if err := json.Unmarshal([]byte(tt.raw), &s); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRateApplyRounding(t *testing.T) {
	t.Parallel()

	r := fxrate.Rate{Base: "EUR", Quote: "USD", Value: d("1.005"), AsOf: asOf, Provider: "test"}

	// 1 × 1.005 = 1.005 — a tie at 2 digits: half-even goes to the even
	// neighbor, half-up away from zero.
	c := r.Apply(d("1"), 2, decimal.HalfEven)
	if got := c.Result.String(); got != "1.00" {
		t.Fatalf("HalfEven Result = %s, want 1.00", got)
	}
	c = r.Apply(d("1"), 2, decimal.HalfUp)
	if got := c.Result.String(); got != "1.01" {
		t.Fatalf("HalfUp Result = %s, want 1.01", got)
	}
}
