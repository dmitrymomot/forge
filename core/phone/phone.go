package phone

import (
	"strings"

	"github.com/dmitrymomot/forge/core/country"
)

// Phone is a normalized E.164 phone number. The zero Phone is the empty value
// (IsZero reports true). It is pointer-free for low GC scan cost.
type Phone struct {
	e164     string // "+14155552671"
	resolved string // alpha-2 when a region hint pinned the country; "" otherwise
	dialLen  int    // dial-code digit count, for splitting E164 into dial + national
}

// primaryDial designates the "main" country for a shared dial code, so Country
// can return a stable primary for an ambiguous number. Codes not listed fall
// back to the first candidate by Name.
var primaryDial = map[string]string{
	"1": "US", "7": "RU", "44": "GB", "39": "IT", "61": "AU", "47": "NO", "212": "MA", "358": "FI",
}

// Parse normalizes a phone number that carries its own country code (a leading +
// or 00). Formatting characters (spaces, dashes, parentheses, dots, slashes) are
// stripped. It returns ErrMissingCountryCode when no + or 00 is present.
func Parse(input string) (Phone, error) {
	digits, err := toDigits(input, true)
	if err != nil {
		return Phone{}, err
	}
	return build(digits, "")
}

// toDigits strips a leading + or 00 and all formatting separators, returning the
// bare digit string. When requireCC is true, absence of a + or 00 is an error.
func toDigits(input string, requireCC bool) (string, error) {
	s := strings.TrimSpace(input)
	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "00"):
		s = s[2:]
	default:
		if requireCC {
			return "", ErrMissingCountryCode
		}
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		ch := s[i]
		switch {
		case ch >= '0' && ch <= '9':
			b.WriteByte(ch)
		case isSep(ch):
			// drop
		default:
			return "", ErrInvalidNumber
		}
	}
	d := b.String()
	if d == "" {
		return "", ErrInvalidNumber
	}
	return d, nil
}

func isSep(ch byte) bool {
	switch ch {
	case ' ', '-', '(', ')', '.', '/':
		return true
	}
	return false
}

// matchDial finds the longest dial-code prefix (1–3 digits) present in country's
// table.
func matchDial(digits string) (string, bool) {
	for n := 3; n >= 1; n-- {
		if len(digits) < n {
			continue
		}
		p := digits[:n]
		if len(country.ByDialCode(p)) > 0 {
			return p, true
		}
	}
	return "", false
}

// build validates E.164 length, resolves the dial code, and constructs a Phone.
// resolved, when non-empty, records a region hint that pinned the country.
func build(digits, resolved string) (Phone, error) {
	if len(digits) > 15 {
		return Phone{}, ErrInvalidNumber
	}
	dial, ok := matchDial(digits)
	if !ok {
		return Phone{}, ErrUnknownDialCode
	}
	if len(digits) <= len(dial) {
		return Phone{}, ErrInvalidNumber // national number empty
	}
	return Phone{e164: "+" + digits, resolved: resolved, dialLen: len(dial)}, nil
}

// E164 returns the canonical E.164 form, e.g. "+14155552671" ("" for the zero
// Phone).
func (p Phone) E164() string { return p.e164 }

// DialCode returns the E.164 country calling code without the +, e.g. "1".
func (p Phone) DialCode() string {
	if p.e164 == "" {
		return ""
	}
	return p.e164[1 : 1+p.dialLen]
}

// NationalNumber returns the significant number after the dial code, e.g.
// "4155552671".
func (p Phone) NationalNumber() string {
	if p.e164 == "" {
		return ""
	}
	return p.e164[1+p.dialLen:]
}

// IsZero reports whether p is the zero Phone.
func (p Phone) IsZero() bool { return p.e164 == "" }

// ParseRegion parses a number using a region hint (an alpha-2 code). Bare
// national input has one leading trunk 0 stripped and the region's dial code
// prepended; input that already carries a + or 00 is parsed as-is, and the
// region, when it is among the dial code's candidates, resolves the country.
// An unknown region yields ErrMissingCountryCode.
//
// Known limitation: the single-leading-0 trunk-prefix rule is near-universal but
// not absolute (NANP uses no trunk 0; some plans keep a significant leading 0) —
// callers in those regions pass fully-qualified + input to Parse.
func ParseRegion(input, alpha2 string) (Phone, error) {
	c, ok := country.ByAlpha2(alpha2)
	if !ok {
		return Phone{}, ErrMissingCountryCode
	}
	s := strings.TrimSpace(input)
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "00") {
		p, err := Parse(s)
		if err != nil {
			return Phone{}, err
		}
		for _, cand := range p.Candidates() {
			if cand.Alpha2 == c.Alpha2 {
				p.resolved = c.Alpha2
				break
			}
		}
		return p, nil
	}
	digits, err := toDigits(s, false)
	if err != nil {
		return Phone{}, err
	}
	digits = strings.TrimPrefix(digits, "0")
	if digits == "" {
		return Phone{}, ErrInvalidNumber
	}
	return build(c.DialCode+digits, c.Alpha2)
}

// Candidates returns every country sharing this number's dial code (nil for the
// zero Phone). It is the escape hatch for the shared-dial-code case Country
// cannot disambiguate.
func (p Phone) Candidates() []country.Country {
	if p.e164 == "" {
		return nil
	}
	return country.ByDialCode(p.DialCode())
}

// Country returns the number's country. The bool is true when the country is
// certain — a dial code used by exactly one country, or a region hint that
// pinned it — and false when the dial code is shared and unresolved, in which
// case a stable primary is still returned (use Candidates for all options).
func (p Phone) Country() (country.Country, bool) {
	if p.e164 == "" {
		return country.Country{}, false
	}
	if p.resolved != "" {
		c, ok := country.ByAlpha2(p.resolved)
		return c, ok
	}
	cs := p.Candidates()
	switch len(cs) {
	case 0:
		return country.Country{}, false
	case 1:
		return cs[0], true
	default:
		if a, ok := primaryDial[p.DialCode()]; ok {
			if c, ok2 := country.ByAlpha2(a); ok2 {
				return c, false
			}
		}
		return cs[0], false
	}
}
