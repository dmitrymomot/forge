package id

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"fmt"
	"time"
)

// UUID is a 128-bit RFC 9562 version-7 identifier: a 48-bit big-endian
// millisecond timestamp, the version and variant bits, and 74 random bits.
type UUID [16]byte

var (
	_ fmt.Stringer             = UUID{}
	_ encoding.TextMarshaler   = UUID{}
	_ encoding.TextUnmarshaler = (*UUID)(nil)
	_ driver.Valuer            = UUID{}
	_ sql.Scanner              = (*UUID)(nil)
)

const (
	hexLower = "0123456789abcdef"
	hexUpper = "0123456789ABCDEF"
)

// Time returns the embedded timestamp truncated to millisecond precision (UTC).
func (u UUID) Time() time.Time { return timeFromMillis(millis(u[:])) }

// IsZero reports whether u is the zero value.
func (u UUID) IsZero() bool { return u == UUID{} }

// String returns the canonical lowercase 8-4-4-4-12 hex form.
func (u UUID) String() string { return u.encode(hexLower) }

// StringLower is an alias for String (the canonical form is lowercase).
func (u UUID) StringLower() string { return u.encode(hexLower) }

// StringUpper returns the uppercase hex form.
func (u UUID) StringUpper() string { return u.encode(hexUpper) }

func (u UUID) encode(digits string) string {
	var b [36]byte
	u.encodeInto(b[:], digits)
	return string(b[:])
}

func (u UUID) encodeInto(dst []byte, digits string) {
	j := 0
	for i := range 16 {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			dst[j] = '-'
			j++
		}
		dst[j] = digits[u[i]>>4]
		dst[j+1] = digits[u[i]&0x0f]
		j += 2
	}
}

// MarshalText implements encoding.TextMarshaler (canonical lowercase form).
func (u UUID) MarshalText() ([]byte, error) {
	b := make([]byte, 36)
	u.encodeInto(b, hexLower)
	return b, nil
}

// UnmarshalText implements encoding.TextUnmarshaler (case-insensitive).
func (u *UUID) UnmarshalText(text []byte) error {
	v, err := ParseUUID(string(text))
	if err != nil {
		return err
	}
	*u = v
	return nil
}

// Value implements driver.Valuer, returning the canonical string so the value
// binds to a native Postgres uuid column through any database/sql driver. pgx
// reaches the raw 16 bytes without this detour once postgres.RegisterIDTypes has
// run, which data/postgres.Open does for every pooled connection.
func (u UUID) Value() (driver.Value, error) { return u.String(), nil }

// Scan implements sql.Scanner from a string, the canonical text as []byte, or
// the raw 16 bytes. A nil source (SQL NULL) yields the zero UUID.
func (u *UUID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*u = UUID{}
		return nil
	case string:
		return u.UnmarshalText([]byte(v))
	case []byte:
		if len(v) == 16 {
			copy(u[:], v)
			return nil
		}
		return u.UnmarshalText(v)
	default:
		return fmt.Errorf("id: cannot scan %T into UUID: %w", src, ErrMalformed)
	}
}

// uuidBytePos maps each of the 16 bytes to its high-nibble index in the
// canonical 36-character string.
var uuidBytePos = [16]int{0, 2, 4, 6, 9, 11, 14, 16, 19, 21, 24, 26, 28, 30, 32, 34}

// ParseUUID parses the canonical 36-character hex form (case-insensitive). It
// accepts any 128-bit value; it does not require the version/variant bits to be 7.
func ParseUUID(s string) (UUID, error) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return UUID{}, fmt.Errorf("id: bad UUID %q: %w", s, ErrMalformed)
	}
	var u UUID
	for i, p := range uuidBytePos {
		hi, ok1 := fromHex(s[p])
		lo, ok2 := fromHex(s[p+1])
		if !ok1 || !ok2 {
			return UUID{}, fmt.Errorf("id: bad UUID %q: %w", s, ErrMalformed)
		}
		u[i] = hi<<4 | lo
	}
	return u, nil
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
