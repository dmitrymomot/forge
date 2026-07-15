package phone

import "github.com/dmitrymomot/forge/core/country"

type config struct {
	allowed       country.Set
	defaultRegion string
	gate          bool
}
