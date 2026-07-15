package phone

import (
	"fmt"
	"slices"

	"github.com/dmitrymomot/forge/core/country"
)

// Parser is a configured phone parser: an optional default region for bare
// national input, and an optional supported-countries gate.
type Parser struct {
	cfg config
}

// New builds a Parser from options. It fails closed on an unknown default
// region, returning ErrMissingCountryCode.
func New(opts ...Option) (*Parser, error) {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if c.defaultRegion != "" {
		if _, ok := country.ByAlpha2(c.defaultRegion); !ok {
			return nil, fmt.Errorf("phone: unknown default region %q: %w", c.defaultRegion, ErrMissingCountryCode)
		}
	}
	return &Parser{cfg: c}, nil
}

// Parse normalizes input, applying the default region (if configured) to bare
// national numbers, then the supported-countries gate (if configured).
func (p *Parser) Parse(input string) (Phone, error) {
	var (
		ph  Phone
		err error
	)
	if p.cfg.defaultRegion != "" {
		ph, err = ParseRegion(input, p.cfg.defaultRegion)
	} else {
		ph, err = Parse(input)
	}
	if err != nil {
		return Phone{}, err
	}
	if p.cfg.gate && !gatePass(p.cfg.allowed, ph) {
		return Phone{}, ErrUnsupportedRegion
	}
	return ph, nil
}

// gatePass reports whether ph is allowed: a resolved/unique country must be in
// the set; an ambiguous number passes when any candidate is in the set (the
// number cannot be proven to belong to the unsupported one).
func gatePass(set country.Set, ph Phone) bool {
	if c, ok := ph.Country(); ok {
		return set.Contains(c)
	}
	return slices.ContainsFunc(ph.Candidates(), set.Contains)
}
