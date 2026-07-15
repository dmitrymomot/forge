// Package phone normalizes phone numbers to E.164 and decomposes them against
// core/country's dial-code table. It parses messy human input into a canonical
// Phone value, formats it back out, and gates parsing by a supported-countries
// policy — deliberately without the libphonenumber machinery.
//
// Parse accepts a number that carries its own country code (a leading + or 00),
// stripping formatting characters. ParseRegion interprets bare national input
// using an alpha-2 region hint (stripping one leading trunk 0). A configured
// Parser (New with WithDefaultRegion and/or WithAllowedCountries) applies a
// default region to bare input and rejects numbers whose country is provably
// outside a country.Set with ErrUnsupportedRegion.
//
// A Phone exposes E164, DialCode, NationalNumber, Country, and Candidates.
// Because dial codes are shared (+1 covers the US, Canada, and the Caribbean),
// Country returns a stable primary with a false ok for an unresolved shared
// code — Candidates lists every possibility, and a region hint pins one. The
// gate mirrors this honesty: an ambiguous number passes when any candidate is
// supported, since it cannot be proven to belong to the unsupported one.
//
// Phone marshals to and from the E.164 string for SQL (Value/Scan, zero Phone
// is NULL) and JSON (zero Phone is null). What this is NOT: no per-country
// length/pattern validation, no line-type or carrier lookup, no pretty
// per-country grouping, and no area-code disambiguation of shared dial codes —
// the libphonenumber swamp stays out. Cheap E.164 shape validation lives in
// core/validate.
//
// # Usage
//
//	p, err := phone.Parse("+1 (415) 555-2671")
//	if err == nil {
//		_ = p.E164()           // "+14155552671"
//		_ = p.NationalNumber() // "4155552671"
//	}
//
//	set, _ := country.NewSetFromCodes("US", "GB", "DE")
//	parser, _ := phone.New(phone.WithDefaultRegion("US"), phone.WithAllowedCountries(set))
//	_, err = parser.Parse("+33 6 12 34 56 78") // ErrUnsupportedRegion
package phone
