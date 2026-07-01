package decimal

import (
	"fmt"
	"math/big"
	"strings"
)

// Parse parses a decimal literal: an optional leading '+'/'-', integer digits,
// an optional single '.', and fractional digits. At least one digit is required
// overall. Scientific notation is NOT supported in v1 and yields ErrSyntax.
func Parse(s string) (Decimal, error) {
	if s == "" {
		return Decimal{}, fmt.Errorf("decimal: parse %q: %w", s, ErrSyntax)
	}

	neg := false
	body := s
	switch body[0] {
	case '+':
		body = body[1:]
	case '-':
		neg = true
		body = body[1:]
	}
	if body == "" {
		return Decimal{}, fmt.Errorf("decimal: parse %q: %w", s, ErrSyntax)
	}

	intPart, fracPart := body, ""
	if dot := strings.IndexByte(body, '.'); dot >= 0 {
		intPart = body[:dot]
		fracPart = body[dot+1:]
		if strings.IndexByte(fracPart, '.') >= 0 {
			return Decimal{}, fmt.Errorf("decimal: parse %q: %w", s, ErrSyntax)
		}
	}
	if intPart == "" && fracPart == "" {
		return Decimal{}, fmt.Errorf("decimal: parse %q: %w", s, ErrSyntax)
	}
	if !allDigits(intPart) || !allDigits(fracPart) {
		return Decimal{}, fmt.Errorf("decimal: parse %q: %w", s, ErrSyntax)
	}

	scale := int32(len(fracPart))
	digits := intPart + fracPart
	if digits == "" {
		return Decimal{}, fmt.Errorf("decimal: parse %q: %w", s, ErrSyntax)
	}

	coef, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return Decimal{}, fmt.Errorf("decimal: parse %q: %w", s, ErrSyntax)
	}
	if neg {
		coef.Neg(coef)
	}
	return fromBig(coef, scale), nil
}

// allDigits reports whether s is empty or contains only ASCII digits.
func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// MustParse is like Parse but panics on error. For package-level test/config
// literals known to be valid.
func MustParse(s string) Decimal {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// String renders d preserving its stored scale: 2.50 → "2.50", 0.001 → "0.001".
func (d Decimal) String() string {
	// Absolute-value coefficient digits, sign handled separately.
	var digits string
	var neg bool
	if d.big != nil {
		neg = d.big.Sign() < 0
		digits = new(big.Int).Abs(d.big).String()
	} else {
		neg = d.coef < 0
		c := d.coef
		if c < 0 {
			// -c can overflow only at MinInt64; big path handles that, so this is safe.
			c = -c
		}
		digits = formatUint(uint64(c))
	}

	if d.scale == 0 {
		if neg && digits != "0" {
			return "-" + digits
		}
		return digits
	}

	scale := int(d.scale)
	// Left-pad so there are at least scale+1 digits (one for the integer part).
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	intPart := digits[:len(digits)-scale]
	fracPart := digits[len(digits)-scale:]
	out := intPart + "." + fracPart
	if neg && !isAllZero(digits) {
		out = "-" + out
	}
	return out
}

// formatUint renders u in base 10 without importing strconv into this hot path
// via a fixed buffer.
func formatUint(u uint64) string {
	if u == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = byte('0' + u%10)
		u /= 10
	}
	return string(buf[i:])
}

// isAllZero reports whether s is non-empty and every rune is '0'.
func isAllZero(s string) bool {
	for i := range len(s) {
		if s[i] != '0' {
			return false
		}
	}
	return len(s) > 0
}

// Float64 returns the value of d as a float64 plus an exact flag reporting
// whether the conversion is lossless. It is best-effort for display/interop only;
// never use it for money math.
func (d Decimal) Float64() (float64, bool) {
	// Exact rational value = coef / 10^scale.
	num := d.bigCoef()
	den := pow10Big(d.scale)
	r := new(big.Rat).SetFrac(num, den)
	f, exact := r.Float64()
	return f, exact
}

// MarshalText implements encoding.TextMarshaler, emitting the same form as String
// (scale-preserving). It never errors.
func (d Decimal) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, parsing the form produced by
// MarshalText/String. Invalid input returns ErrSyntax.
func (d *Decimal) UnmarshalText(p []byte) error {
	v, err := Parse(string(p))
	if err != nil {
		return err
	}
	*d = v
	return nil
}
