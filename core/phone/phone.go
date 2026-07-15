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
