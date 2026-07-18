package i18n

import (
	"bytes"
	"math"
	"strconv"
)

// FormatSpec holds one locale's rendering conventions. This package ships no
// per-locale values — only Invariant. Real CLDR specs live in core/i18n/cldr
// and are wired with WithFormat.
type FormatSpec struct {
	// DecimalSep separates integer and fractional digits ("." or ",").
	DecimalSep string
	// GroupSep separates 3-digit groups ("," "." or U+00A0).
	GroupSep string
	// DateLayout, TimeLayout, DateTimeLayout are Go time layouts.
	DateLayout     string
	TimeLayout     string
	DateTimeLayout string
	// CurrencyBefore places the symbol before the amount ($1.50 vs 1,50 €).
	CurrencyBefore bool
	// CurrencySpace inserts a space between symbol and amount.
	CurrencySpace bool
	// PercentSpace inserts a space before the percent sign (fr: "50 %").
	PercentSpace bool
}

// Invariant is the FormatSpec applied to any locale with no wired spec: a
// neutral, ISO-8601 rendering. It is not a claim that any locale looks like
// this — it is the honest default for a package that knows no locales.
var Invariant = FormatSpec{
	DecimalSep:     ".",
	GroupSep:       ",",
	DateLayout:     "2006-01-02",
	TimeLayout:     "15:04",
	DateTimeLayout: "2006-01-02 15:04",
	CurrencyBefore: true,
}

// completeSpec fills a partial FormatSpec's empty layout and decimal-separator
// fields from Invariant. A caller who wires only some fields — just the
// separators, say — would otherwise feed a zero DateLayout straight into
// time.AppendFormat, which renders the empty string with no error; completion
// keeps such a spec producing real dates and numbers. GroupSep is deliberately
// left untouched: an empty group separator is a valid choice (no digit
// grouping), unlike an empty decimal separator, which would fuse a number's
// integer and fraction digits. Fully-wired specs (every cldr.Format*) pass
// through unchanged.
func completeSpec(s FormatSpec) FormatSpec {
	if s.DecimalSep == "" {
		s.DecimalSep = Invariant.DecimalSep
	}
	if s.DateLayout == "" {
		s.DateLayout = Invariant.DateLayout
	}
	if s.TimeLayout == "" {
		s.TimeLayout = Invariant.TimeLayout
	}
	if s.DateTimeLayout == "" {
		s.DateTimeLayout = Invariant.DateTimeLayout
	}
	return s
}

// specFor returns the resolved FormatSpec for the locale at index idx: exact
// tag, then base language, then Invariant, as decided once at New (see
// resolveFormat) and stored per locale. Bundle is immutable after New, so
// this address is stable for the Bundle's whole lifetime.
func (b *Bundle) specFor(idx int) *FormatSpec {
	return &b.locales[idx].format
}

// Number formats v with the locale's separators. It does not round: the
// shortest representation that round-trips is used, so a caller wanting fixed
// decimals rounds first. Currency is the method that rounds.
func (b *Bundle) Number(loc Locale, v float64) string {
	return string(b.AppendNumber(make([]byte, 0, 32), loc, v))
}

// AppendNumber is Number appending into dst.
func (b *Bundle) AppendNumber(dst []byte, loc Locale, v float64) []byte {
	return appendNumberSpec(dst, v, b.specFor(b.locIdx(loc)))
}

// NumberInt formats n with the locale's grouping separator.
func (b *Bundle) NumberInt(loc Locale, n int64) string {
	return string(b.AppendNumberInt(make([]byte, 0, 24), loc, n))
}

// AppendNumberInt is NumberInt appending into dst.
func (b *Bundle) AppendNumberInt(dst []byte, loc Locale, n int64) []byte {
	return appendIntSpec(dst, n, b.specFor(b.locIdx(loc)))
}

// Percent formats a ratio as a percentage: 0.5 renders "50%", or "50 %" where
// the locale's spec sets PercentSpace.
func (b *Bundle) Percent(loc Locale, ratio float64) string {
	return string(b.AppendPercent(make([]byte, 0, 16), loc, ratio))
}

// AppendPercent is Percent appending into dst.
func (b *Bundle) AppendPercent(dst []byte, loc Locale, ratio float64) []byte {
	return appendPercentSpec(dst, ratio, b.specFor(b.locIdx(loc)))
}

// absU64 returns |n| as a uint64. A plain negation overflows at
// math.MinInt64 (its magnitude has no int64 representation); routing through
// uint64 first makes the conversion total for every int64 input. Shared by
// every int→magnitude conversion in this package (currency reuses it).
func absU64(n int64) uint64 {
	if n < 0 {
		return uint64(-(n + 1)) + 1
	}
	return uint64(n)
}

// appendIntSpec renders a signed integer with grouping.
func appendIntSpec(dst []byte, n int64, s *FormatSpec) []byte {
	if n < 0 {
		dst = append(dst, '-')
	}
	return appendGroupedUint(dst, absU64(n), s.GroupSep)
}

// appendGroupedUint writes n with a separator every three digits. It formats
// back-to-front into buf, sized to fit 20 uint64 digits plus up to 7
// separators. For a normal (short) GroupSep that fits in the fixed stack
// array, formatting never allocates. GroupSep is a public FormatSpec field an
// app can set to any string, so a pathological long separator must not
// overflow the buffer — it falls back to a heap allocation sized to fit
// instead, keeping rendering total.
func appendGroupedUint(dst []byte, n uint64, groupSep string) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	var stack [64]byte
	buf := stack[:]
	if need := 20 + 7*len(groupSep); need > len(buf) {
		buf = make([]byte, need)
	}
	pos := len(buf)
	digits := 0
	for n > 0 {
		if digits > 0 && digits%3 == 0 && groupSep != "" {
			pos -= len(groupSep)
			copy(buf[pos:], groupSep)
		}
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
		digits++
	}
	return append(dst, buf[pos:]...)
}

// appendNumberSpec renders a float with grouping and the locale's decimal
// separator. NaN and ±Inf are rendered as themselves — strconv's shortest
// round-tripping form for these is "NaN"/"+Inf"/"-Inf", non-digit text that
// the grouping logic below is not meant to see, so they are special-cased
// ahead of it rather than fed through it.
//
// For finite values, strconv produces the shortest round-tripping form; the
// integer part is then regrouped and the '.' swapped for the locale's
// separator.
func appendNumberSpec(dst []byte, v float64, s *FormatSpec) []byte {
	switch {
	case math.IsNaN(v):
		return append(dst, "NaN"...)
	case math.IsInf(v, 1):
		return append(dst, "+Inf"...)
	case math.IsInf(v, -1):
		return append(dst, "-Inf"...)
	}

	var tmp [40]byte
	raw := strconv.AppendFloat(tmp[:0], v, 'f', -1, 64)

	neg := len(raw) > 0 && raw[0] == '-'
	if neg {
		dst = append(dst, '-')
		raw = raw[1:]
	}
	intPart, frac, _ := bytes.Cut(raw, []byte{'.'})
	dst = appendGroupedDigits(dst, intPart, s.GroupSep)
	if len(frac) > 0 {
		dst = append(dst, s.DecimalSep...)
		dst = append(dst, frac...)
	}
	return dst
}

// appendGroupedDigits regroups an already-rendered digit string.
func appendGroupedDigits(dst []byte, digits []byte, groupSep string) []byte {
	if digits == nil {
		return append(dst, '0')
	}
	n := len(digits)
	if n == 0 {
		return append(dst, '0')
	}
	lead := n % 3
	if lead == 0 {
		lead = 3
	}
	dst = append(dst, digits[:lead]...)
	for i := lead; i < n; i += 3 {
		if groupSep != "" {
			dst = append(dst, groupSep...)
		}
		dst = append(dst, digits[i:i+3]...)
	}
	return dst
}

// percentFractionScale rounds a scaled percentage to six fraction digits; see
// appendPercentSpec.
const percentFractionScale = 1e6

// appendPercentSpec renders ratio*100 with a percent sign. Scaling the ratio by
// 100 in float64 turns exact ratios into near misses — 0.29*100 is
// 28.999999999999996, not 29 — which the shortest-round-trip renderer would
// then print in full. Rounding the scaled value to a fixed decimal grid first
// collapses that error back to the intended figure; a percentage is a display
// quantity that never needs more than these few fraction digits, so unlike
// Number this drops no precision anyone would want. NaN and ±Inf pass through
// the arithmetic unchanged, so appendNumberSpec still renders them verbatim.
func appendPercentSpec(dst []byte, ratio float64, s *FormatSpec) []byte {
	p := math.Round(ratio*100*percentFractionScale) / percentFractionScale
	dst = appendNumberSpec(dst, p, s)
	if s.PercentSpace {
		dst = append(dst, ' ')
	}
	return append(dst, '%')
}
