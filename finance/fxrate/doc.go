// Package fxrate provides exchange rates behind a RateSource seam with stored
// snapshots. Convert records the rate applied — provider, as-of time, value,
// rounding — so audits answer "what rate at transaction time" and any stored
// Conversion or Snapshot recomputes byte-for-byte.
//
// Math is exact multiply-and-round via core/decimal: amount × rate, rounded
// once to the caller's scale and mode. Rates derived by division (inverse and
// cross rates through the snapshot base) use RateScale digits half-to-even —
// a package constant, so derivations are deterministic forever. Nothing ever
// routes through float64.
//
// Providers are thin JSON adapters over web/httpclient implementing
// RateSource (see the frankfurter subpackage); there are no provider SDKs and
// no live streaming. StaticSource covers tests and fixed contractual rates.
//
// The Converter caches one snapshot and refreshes it through the source when
// older than the TTL, coalescing concurrent refreshes into a single fetch. It
// fails closed: a stale snapshot plus a failing source is an error, never
// silently-stale rates. Rates are market data shared by all tenants — one
// Converter serves a multi-tenant app; per-tenant negotiated rates are
// separate Converter or StaticSource values chosen by the caller.
//
// # Usage
//
//	src := frankfurter.New()
//	conv, err := fxrate.New(src, "EUR",
//		fxrate.WithTTL(time.Hour),
//		fxrate.WithQuotes("USD", "GBP"),
//	)
//	if err != nil {
//		// invalid configuration
//	}
//
//	c, err := conv.Convert(ctx, decimal.MustParse("99.90"), "USD", "GBP", 2, decimal.HalfEven)
//	if err != nil {
//		// unknown currency, or the source failed while the cache was stale
//	}
//	_ = c.Result       // GBP amount, rounded once to 2 digits
//	_ = c.Rate.Value   // the exact rate applied
//	_ = c.Rate.AsOf    // when the provider published it
//
// Persist the Conversion (or the whole Snapshot from conv.Snapshot) with the
// transaction; replaying Amount × Rate.Value with the recorded scale and mode
// reproduces Result exactly.
package fxrate
