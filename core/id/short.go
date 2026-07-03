package id

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"fmt"
	"time"
)

// Short is an 80-bit sortable identifier: a 48-bit big-endian millisecond
// timestamp followed by 32 random bits, rendered as 16 Crockford base32
// characters. It is the compact, URL-safe option for link shorteners and similar.
type Short [10]byte

var (
	_ fmt.Stringer             = Short{}
	_ encoding.TextMarshaler   = Short{}
	_ encoding.TextUnmarshaler = (*Short)(nil)
	_ driver.Valuer            = Short{}
	_ sql.Scanner              = (*Short)(nil)
)

// Time returns the embedded timestamp truncated to millisecond precision (UTC).
func (s Short) Time() time.Time { return timeFromMillis(millis(s[:])) }

// IsZero reports whether s is the zero value.
func (s Short) IsZero() bool { return s == Short{} }

// String returns the canonical uppercase 16-character Crockford base32 form.
func (s Short) String() string { return s.encode(crockford) }

// StringUpper is an alias for String (the canonical form is uppercase).
func (s Short) StringUpper() string { return s.encode(crockford) }

// StringLower returns the lowercase Crockford base32 form (handy for URLs).
func (s Short) StringLower() string { return s.encode(crockfordLower) }

func (s Short) encode(alpha string) string {
	var b [16]byte
	encodeBase32(b[:], s[:], alpha)
	return string(b[:])
}

// MarshalText implements encoding.TextMarshaler (canonical uppercase form).
func (s Short) MarshalText() ([]byte, error) {
	b := make([]byte, 16)
	encodeBase32(b, s[:], crockford)
	return b, nil
}

// UnmarshalText implements encoding.TextUnmarshaler (case-insensitive).
func (s *Short) UnmarshalText(text []byte) error {
	v, err := ParseShort(string(text))
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// Value implements driver.Valuer, returning the canonical string for a text column.
func (s Short) Value() (driver.Value, error) { return s.String(), nil }

// Scan implements sql.Scanner from a string or its text as []byte. A nil source
// (SQL NULL) yields the zero Short.
func (s *Short) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*s = Short{}
		return nil
	case string:
		return s.UnmarshalText([]byte(v))
	case []byte:
		return s.UnmarshalText(v)
	default:
		return fmt.Errorf("id: cannot scan %T into Short: %w", src, ErrMalformed)
	}
}

// ParseShort parses the 16-character Crockford base32 form (case-insensitive).
func ParseShort(s string) (Short, error) {
	var out Short
	if len(s) != 16 || !decodeBase32(out[:], s) {
		return Short{}, fmt.Errorf("id: bad Short %q: %w", s, ErrMalformed)
	}
	return out, nil
}
