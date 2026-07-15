package phone

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// Value implements driver.Valuer, storing the E.164 string. The zero Phone
// stores SQL NULL.
func (p Phone) Value() (driver.Value, error) {
	if p.e164 == "" {
		return nil, nil
	}
	return p.e164, nil
}

// Scan implements sql.Scanner for the E.164 string form. A nil or empty source
// yields the zero Phone; any other value is re-parsed by Parse.
func (p *Phone) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*p = Phone{}
		return nil
	case string:
		return p.scan(v)
	case []byte:
		return p.scan(string(v))
	default:
		return fmt.Errorf("phone: cannot scan %T: %w", src, ErrInvalidNumber)
	}
}

func (p *Phone) scan(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		*p = Phone{}
		return nil
	}
	parsed, err := Parse(s)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
