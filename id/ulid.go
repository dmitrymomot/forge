package id

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"fmt"
	"time"
)

// ULID is a 128-bit Universally Unique Lexicographically Sortable Identifier: a
// 48-bit big-endian millisecond timestamp followed by 80 random bits, rendered
// as 26 Crockford base32 characters.
type ULID [16]byte

var (
	_ fmt.Stringer             = ULID{}
	_ encoding.TextMarshaler   = ULID{}
	_ encoding.TextUnmarshaler = (*ULID)(nil)
	_ driver.Valuer            = ULID{}
	_ sql.Scanner              = (*ULID)(nil)
)

// Time returns the embedded timestamp truncated to millisecond precision (UTC).
func (u ULID) Time() time.Time { return timeFromMillis(millis(u[:])) }

// IsZero reports whether u is the zero value.
func (u ULID) IsZero() bool { return u == ULID{} }

// String returns the canonical uppercase 26-character Crockford base32 form.
func (u ULID) String() string { return u.encode(crockford) }

// StringUpper is an alias for String (the canonical form is uppercase).
func (u ULID) StringUpper() string { return u.encode(crockford) }

// StringLower returns the lowercase Crockford base32 form.
func (u ULID) StringLower() string { return u.encode(crockfordLower) }

func (u ULID) encode(alpha string) string {
	var b [26]byte
	encodeBase32(b[:], u[:], alpha)
	return string(b[:])
}

// MarshalText implements encoding.TextMarshaler (canonical uppercase form).
func (u ULID) MarshalText() ([]byte, error) {
	b := make([]byte, 26)
	encodeBase32(b, u[:], crockford)
	return b, nil
}

// UnmarshalText implements encoding.TextUnmarshaler (case-insensitive).
func (u *ULID) UnmarshalText(text []byte) error {
	v, err := ParseULID(string(text))
	if err != nil {
		return err
	}
	*u = v
	return nil
}

// Value implements driver.Valuer, returning the canonical string for a text column.
func (u ULID) Value() (driver.Value, error) { return u.String(), nil }

// Scan implements sql.Scanner from a string or its text as []byte. A nil source
// (SQL NULL) yields the zero ULID.
func (u *ULID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*u = ULID{}
		return nil
	case string:
		return u.UnmarshalText([]byte(v))
	case []byte:
		return u.UnmarshalText(v)
	default:
		return fmt.Errorf("id: cannot scan %T into ULID: %w", src, ErrMalformed)
	}
}

// ParseULID parses the 26-character Crockford base32 form (case-insensitive).
func ParseULID(s string) (ULID, error) {
	var u ULID
	if len(s) != 26 || !decodeBase32(u[:], s) {
		return ULID{}, fmt.Errorf("id: bad ULID %q: %w", s, ErrMalformed)
	}
	return u, nil
}
