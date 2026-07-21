package fxrate_test

import (
	"context"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/core/decimal"
	"github.com/dmitrymomot/forge/finance/fxrate"
)

func Example() {
	// In production the source is a provider adapter (e.g. the frankfurter
	// subpackage); StaticSource serves fixed rates.
	snap, err := fxrate.NewSnapshot("EUR", "example", time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC), map[string]decimal.Decimal{
		"USD": decimal.MustParse("1.0850"),
		"GBP": decimal.MustParse("0.8425"),
	})
	if err != nil {
		panic(err)
	}
	src, err := fxrate.NewStaticSource(snap)
	if err != nil {
		panic(err)
	}

	conv, err := fxrate.New(src, "EUR", fxrate.WithTTL(time.Hour))
	if err != nil {
		panic(err)
	}

	c, err := conv.Convert(context.Background(), decimal.MustParse("100.00"), "EUR", "USD", 2, decimal.HalfEven)
	if err != nil {
		panic(err)
	}

	// The Conversion records the applied rate — persist it with the
	// transaction and the result recomputes byte-for-byte.
	fmt.Println(c.Result, "USD at", c.Rate.Value, "from", c.Rate.Provider)
	fmt.Println(c.Amount.Mul(c.Rate.Value).Round(c.Scale, c.Mode).Equal(c.Result))
	// Output:
	// 108.50 USD at 1.0850 from example
	// true
}
