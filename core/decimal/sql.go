package decimal

import (
	"database/sql/driver"
	"fmt"
)

// Value implements driver.Valuer, serializing the decimal as its exact,
// scale-preserving String form for storage in e.g. a Postgres NUMERIC column.
func (d Decimal) Value() (driver.Value, error) {
	return d.String(), nil
}

// Scan implements sql.Scanner. It accepts the string and []byte forms a SQL
// NUMERIC column yields (parsed exactly via Parse) and int64 (exact). A float64
// is rejected with ErrScan: reading an exact column through a binary float would
// silently import rounding error, so read NUMERIC as text (pgx does) or convert
// deliberately. A nil source is an error too — use NullDecimal for nullable
// columns.
func (d *Decimal) Scan(src any) error {
	switch v := src.(type) {
	case string:
		p, err := Parse(v)
		if err != nil {
			return err
		}
		*d = p
		return nil
	case []byte:
		p, err := Parse(string(v))
		if err != nil {
			return err
		}
		*d = p
		return nil
	case int64:
		*d = FromInt(v)
		return nil
	case float64:
		return fmt.Errorf("decimal: refusing lossy float64 %v: %w", v, ErrScan)
	case nil:
		return fmt.Errorf("decimal: cannot scan nil (use NullDecimal): %w", ErrScan)
	default:
		return fmt.Errorf("decimal: cannot scan %T: %w", src, ErrScan)
	}
}

// NullDecimal is a Decimal that may be null, mirroring sql.NullString. Valid is
// true when Decimal holds a (non-null) value.
type NullDecimal struct {
	Decimal Decimal
	Valid   bool
}

// Scan implements sql.Scanner: a nil source sets the zero Decimal with
// Valid=false; any other source delegates to Decimal.Scan and sets Valid=true.
func (n *NullDecimal) Scan(src any) error {
	if src == nil {
		n.Decimal, n.Valid = Decimal{}, false
		return nil
	}
	if err := n.Decimal.Scan(src); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

// Value implements driver.Valuer: it returns nil when not Valid, otherwise the
// underlying Decimal's Value.
func (n NullDecimal) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Decimal.Value()
}
