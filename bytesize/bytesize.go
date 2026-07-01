package bytesize

import (
	"fmt"
	"strconv"
	"strings"
)

// ByteSize is a number of bytes.
type ByteSize int64

// SI (powers of 1000) and IEC (powers of 1024) unit multipliers.
const (
	B   ByteSize = 1
	KB  ByteSize = 1000
	MB  ByteSize = 1000 * KB
	GB  ByteSize = 1000 * MB
	TB  ByteSize = 1000 * GB
	PB  ByteSize = 1000 * TB
	KiB ByteSize = 1024
	MiB ByteSize = 1024 * KiB
	GiB ByteSize = 1024 * MiB
	TiB ByteSize = 1024 * GiB
	PiB ByteSize = 1024 * TiB
)

// parseUnits are matched against the uppercased input, longest first so IEC
// suffixes win before their SI prefixes and the bare "B".
var parseUnits = []struct {
	suffix string
	mult   ByteSize
}{
	{"PIB", PiB}, {"TIB", TiB}, {"GIB", GiB}, {"MIB", MiB}, {"KIB", KiB},
	{"PB", PB}, {"TB", TB}, {"GB", GB}, {"MB", MB}, {"KB", KB},
	{"B", B},
}

// Parse converts a human byte-size string ("10MB", "1.5GiB", "512", "10 MB")
// into a ByteSize. Suffixes are case-insensitive; a bare number is bytes.
func Parse(s string) (ByteSize, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, s)
	}
	upper := strings.ToUpper(trimmed)
	for _, u := range parseUnits {
		if strings.HasSuffix(upper, u.suffix) {
			num := strings.TrimSpace(upper[:len(upper)-len(u.suffix)])
			return parseNumber(num, u.mult, s)
		}
	}
	return parseNumber(upper, B, s)
}

func parseNumber(num string, mult ByteSize, orig string) (ByteSize, error) {
	if num == "" {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, orig)
	}
	if i, err := strconv.ParseInt(num, 10, 64); err == nil {
		return ByteSize(i) * mult, nil
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, orig)
	}
	return ByteSize(f * float64(mult)), nil
}

// String formats b using IEC units (the default for infra config).
func (b ByteSize) String() string { return FormatIEC(b) }

// FormatIEC formats b with binary (1024) units: B, KiB, MiB, GiB, TiB, PiB.
func FormatIEC(b ByteSize) string {
	return format(int64(b), 1024, []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"})
}

// FormatSI formats b with decimal (1000) units: B, KB, MB, GB, TB, PB.
func FormatSI(b ByteSize) string {
	return format(int64(b), 1000, []string{"B", "KB", "MB", "GB", "TB", "PB"})
}

// format renders n in the largest unit in which it is exact to at most two
// decimals, falling back to a byte count so the output always round-trips
// through Parse. rem*100 cannot overflow int64: rem < base^i <= 1024^5.
func format(n int64, base int64, units []string) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	// Climb to the largest unit whose multiplier does not exceed n.
	pow := int64(1)
	i := 0
	for i+1 < len(units) && n >= pow*base {
		pow *= base
		i++
	}
	// Step back down until the value is exact to at most two decimals.
	for i > 0 {
		rem := n % pow
		if rem == 0 || (rem*100)%pow == 0 {
			break
		}
		pow /= base
		i--
	}
	if i == 0 {
		return sign + strconv.FormatInt(n, 10) + units[0]
	}
	whole := n / pow
	rem := n % pow
	if rem == 0 {
		return sign + strconv.FormatInt(whole, 10) + units[i]
	}
	frac := (rem * 100) / pow // 0..99, exact by the loop's invariant
	s := fmt.Sprintf("%d.%02d", whole, frac)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return sign + s + units[i]
}

// MarshalText renders b as its String form.
func (b ByteSize) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

// UnmarshalText parses p into b.
func (b *ByteSize) UnmarshalText(p []byte) error {
	v, err := Parse(string(p))
	if err != nil {
		return err
	}
	*b = v
	return nil
}
