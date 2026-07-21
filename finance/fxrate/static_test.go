package fxrate_test

import (
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/fxrate"
)

func TestNewStaticSourceRejectsZeroSnapshot(t *testing.T) {
	t.Parallel()

	if _, err := fxrate.NewStaticSource(fxrate.Snapshot{}); !errors.Is(err, fxrate.ErrInvalidSnapshot) {
		t.Fatalf("got %v, want ErrInvalidSnapshot", err)
	}
}

func TestStaticSourceFetch(t *testing.T) {
	t.Parallel()

	snap := mustSnapshot(t, "EUR", map[string]decimal.Decimal{"USD": d("1.0850"), "GBP": d("0.8425"), "JPY": d("160")})
	src, err := fxrate.NewStaticSource(snap)
	if err != nil {
		t.Fatalf("NewStaticSource: %v", err)
	}

	t.Run("all quotes", func(t *testing.T) {
		t.Parallel()
		got, err := src.Fetch(t.Context(), "eur", nil)
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if len(got.Currencies()) != 4 {
			t.Fatalf("Currencies = %v, want 4", got.Currencies())
		}
	})

	t.Run("narrowed quotes", func(t *testing.T) {
		t.Parallel()
		got, err := src.Fetch(t.Context(), "EUR", []string{"usd", "EUR"})
		if err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if got.Has("GBP") || got.Has("JPY") {
			t.Fatalf("not narrowed: %v", got.Currencies())
		}
		r, err := got.Rate("EUR", "USD")
		if err != nil {
			t.Fatalf("Rate: %v", err)
		}
		if !r.Value.Equal(d("1.0850")) {
			t.Fatalf("Rate = %s, want 1.0850", r.Value)
		}
	})

	t.Run("base mismatch", func(t *testing.T) {
		t.Parallel()
		if _, err := src.Fetch(t.Context(), "USD", nil); !errors.Is(err, fxrate.ErrBaseMismatch) {
			t.Fatalf("got %v, want ErrBaseMismatch", err)
		}
	})

	t.Run("missing quote", func(t *testing.T) {
		t.Parallel()
		if _, err := src.Fetch(t.Context(), "EUR", []string{"CHF"}); !errors.Is(err, fxrate.ErrUnknownCurrency) {
			t.Fatalf("got %v, want ErrUnknownCurrency", err)
		}
	})
}
