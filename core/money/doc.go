// Package money provides a currency-aware Money value type built on
// decimal.Decimal, with exact arithmetic and penny-perfect allocation.
//
// A Money couples an exact decimal amount with a Currency (ISO-4217 metadata:
// alphabetic Code, numeric Num, MinorUnits, and a display Symbol). Amounts are
// stored as decimal.Decimal — not integer cents — so percentage and tax math
// stay exact; MinorUnits governs rounding only at settlement and display
// (Minor, Round, String, Allocate, Split). Add, Sub, and Cmp require matching
// currencies and return ErrCurrencyMismatch otherwise. Allocate splits an
// amount proportionally to integer ratios using the largest-remainder method so
// that the parts always sum back to the original exactly.
//
// The full ISO-4217 minor-units table ships as static data (currency_data.go),
// exposed as package-level Currency vars (USD, EUR, GBP, JPY, …) and looked up
// by CurrencyByCode. Symbols are filled for common currencies; otherwise Symbol
// equals Code.
//
// What this is NOT: it does not do FX/currency conversion, and it does not do
// locale-aware formatting — String renders the unambiguous, locale-free
// "1.50 USD" form, and localized symbol/grouping formatting is deferred to a
// future i18n layer. It is not an integer-cents money type; exactness comes from
// the decimal sibling.
//
// Siblings: decimal (the exact fixed-point substrate this builds on); enum
// (closed string value-domains); validate (CurrencyCode/CountryCode shape
// checks). See docs/packages.md for the package catalog.
//
// # Usage
//
//	price := money.FromMinor(1999, money.USD) // 19.99 USD
//	shipping := money.FromMinor(500, money.USD)
//	total, err := price.Add(shipping)
//	if err != nil {
//		// price and shipping share a currency here, so this is unreachable
//	}
//	_ = total.String() // "24.99 USD"
package money
