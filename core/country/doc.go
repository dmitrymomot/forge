// Package country provides curated ISO-3166-1 static data — alpha-2, alpha-3,
// and numeric codes, English short name, primary official ISO-4217 currency
// code, E.164 dial code, and flag emoji — plus case-insensitive lookups and a
// Set type expressing a "supported countries" policy.
//
// Every country is exposed as a package-level Country var (US, GB, DE, …) and
// indexed for lookup by ByAlpha2, ByAlpha3, ByNumeric, and ByDialCode; All
// returns the whole table sorted by Name (a dropdown source). Because many
// countries share one dial code (+1 covers the US, Canada, and the Caribbean),
// ByDialCode returns a slice. The data is committed static — no runtime fetch;
// flag emoji are derived from the alpha-2 pair at package init.
//
// A Set is an explicit, immutable collection a consumer constructs (NewSet, or
// NewSetFromCodes over configuration strings, which fails closed on an unknown
// code) and passes wherever a supported-countries policy is needed — the
// filtered All dropdown, or core/phone's parse gate. The zero Set is a valid
// empty set that contains nothing, so a gate configured with it fails closed.
//
// What this is NOT: it carries only the primary official currency per country
// (not de-facto multi-currency reality), no ISO-3166-2 subdivisions/states, no
// translated names (that is an i18n concern), and no historical or deprecated
// codes. Cheap shape-only validation of a country code lives in core/validate;
// this package is the authoritative data.
//
// # Usage
//
//	c, ok := country.ByAlpha2("us")
//	if ok {
//		_ = c.Name     // "United States"
//		_ = c.Currency // "USD"
//		_ = c.Emoji    // "🇺🇸"
//	}
//
//	supported, _ := country.NewSetFromCodes("US", "GB", "DE")
//	_ = supported.ContainsCode("fr") // false
//	_ = supported.All()              // sorted, for a filtered dropdown
package country
