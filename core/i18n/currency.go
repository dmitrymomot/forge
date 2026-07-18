package i18n

import (
	"github.com/dmitrymomot/forge/core/money"
)

// maxCurrencyDigits bounds the divisor computation in appendMinorMag to
// uint64's safe decimal range (10^19 < 2^64 <= 10^20). No real ISO-4217
// currency has more than 4 minor units (CLF); this is a defensive backstop
// against a pathological Currency value, not a realistic path. Without it, a
// MinorUnits at or beyond 20 would overflow the repeated-multiply divisor —
// which can silently wrap to exactly zero — turning the following division
// into a panic.
const maxCurrencyDigits = 19

// Currency formats m for the locale. The amount and symbol come from money —
// rounded to the currency's minor units — and the locale contributes only
// separators and symbol placement. A de-DE reader viewing USD therefore sees
// "1.234,56 $", which is correct: multi-currency rendering falls out of the
// split for free.
func (b *Bundle) Currency(loc Locale, m money.Money) string {
	return string(b.AppendCurrency(make([]byte, 0, 32), loc, m))
}

// AppendCurrency is Currency appending into dst.
func (b *Bundle) AppendCurrency(dst []byte, loc Locale, m money.Money) []byte {
	return appendCurrencySpec(dst, m, b.specFor(b.locIdx(loc)))
}

// appendCurrencySpec renders m under s. Shared by Bundle and Localizer
// (Task 10), which must not re-implement this.
func appendCurrencySpec(dst []byte, m money.Money, s *FormatSpec) []byte {
	cur := m.Currency()
	minor, ok := m.MinorOK()
	if !ok {
		// The amount does not fit an int64 minor representation. Fail closed
		// with money's own rendering rather than emitting a wrong number.
		return append(dst, m.String()...)
	}

	// The sign always leads, whichever side the symbol sits on: "-$1.00" and
	// "-1,00 €". Everything past this point sees only the magnitude.
	if minor < 0 {
		dst = append(dst, '-')
	}
	mag := absU64(minor) // shared with Number/Percent; see format.go

	if s.CurrencyBefore {
		dst = append(dst, cur.Symbol...)
		if s.CurrencySpace {
			dst = append(dst, ' ')
		}
		return appendMinorMag(dst, mag, cur.MinorUnits, s)
	}
	dst = appendMinorMag(dst, mag, cur.MinorUnits, s)
	if s.CurrencySpace {
		dst = append(dst, ' ')
	}
	return append(dst, cur.Symbol...)
}

// appendMinorMag renders a minor-unit magnitude as major.minor under s. The
// caller has already emitted any sign. units <= 0 (JPY's 0, or a malformed
// negative value) renders no fractional part at all.
func appendMinorMag(dst []byte, mag uint64, units int32, s *FormatSpec) []byte {
	if units <= 0 {
		return appendGroupedUint(dst, mag, s.GroupSep)
	}
	if units > maxCurrencyDigits {
		units = maxCurrencyDigits
	}
	div := uint64(1)
	for range units {
		div *= 10
	}
	dst = appendGroupedUint(dst, mag/div, s.GroupSep)
	dst = append(dst, s.DecimalSep...)
	// Emit exactly `units` fractional digits, most significant first. frac <
	// div always holds, so this both pads and truncates correctly: 5 minor
	// USD is "05", 0 is "00", 5 minor BHD is "005". No branches, no
	// allocation.
	frac := mag % div
	for p := div / 10; p > 0; p /= 10 {
		dst = append(dst, byte('0'+(frac/p)%10))
	}
	return dst
}
