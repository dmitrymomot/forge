package phone

import "github.com/dmitrymomot/forge/core/country"

// Option configures a Parser.
type Option func(*config)

// WithDefaultRegion sets the alpha-2 region used to interpret bare national
// input (numbers with no + or 00). New rejects an unknown region.
func WithDefaultRegion(alpha2 string) Option {
	return func(c *config) { c.defaultRegion = alpha2 }
}

// WithAllowedCountries enables the supported-countries gate: Parse rejects a
// number whose country is provably outside the set with ErrUnsupportedRegion.
// Passing the zero Set fails closed (rejects everything).
func WithAllowedCountries(s country.Set) Option {
	return func(c *config) {
		c.allowed = s
		c.gate = true
	}
}
