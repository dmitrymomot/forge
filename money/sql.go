package money

import (
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/dmitrymomot/forge/decimal"
)

// Value implements driver.Valuer, serializing Money as the composite text form
// "<amount> <code>", e.g. "1.50 USD". The amount is the exact, full-precision
// decimal string (NOT rounded to minor units). A Money with no currency code
// cannot be serialized and returns ErrScan.
func (m Money) Value() (driver.Value, error) {
	if m.currency.Code == "" {
		return nil, fmt.Errorf("money: cannot serialize money with empty currency: %w", ErrScan)
	}
	return m.amount.String() + " " + m.currency.Code, nil
}

// Scan implements sql.Scanner for the "<amount> <code>" text form produced by
// Value. It accepts string and []byte. A nil source is an error — use NullMoney
// for nullable columns.
func (m *Money) Scan(src any) error {
	switch v := src.(type) {
	case string:
		return m.scanString(v)
	case []byte:
		return m.scanString(string(v))
	case nil:
		return fmt.Errorf("money: cannot scan nil (use NullMoney): %w", ErrScan)
	default:
		return fmt.Errorf("money: cannot scan %T: %w", src, ErrScan)
	}
}

// scanString parses the "<amount> <code>" form: everything before the last
// space is the exact amount, and the trailing token is the ISO-4217 code.
func (m *Money) scanString(s string) error {
	s = strings.TrimSpace(s)
	i := strings.LastIndexByte(s, ' ')
	if i < 0 {
		return fmt.Errorf("money: cannot parse %q as \"<amount> <code>\": %w", s, ErrScan)
	}
	amt, err := decimal.Parse(strings.TrimSpace(s[:i]))
	if err != nil {
		return err
	}
	c, ok := CurrencyByCode(s[i+1:])
	if !ok {
		return ErrUnknownCurrency
	}
	m.amount, m.currency = amt, c
	return nil
}

// NullMoney is a Money that may be null, mirroring sql.NullString. Valid is true
// when Money holds a (non-null) value.
type NullMoney struct {
	Money Money
	Valid bool
}

// Scan implements sql.Scanner: a nil source sets the zero Money with
// Valid=false; any other source delegates to Money.Scan and sets Valid=true.
func (n *NullMoney) Scan(src any) error {
	if src == nil {
		n.Money, n.Valid = Money{}, false
		return nil
	}
	if err := n.Money.Scan(src); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

// Value implements driver.Valuer: it returns nil when not Valid, otherwise the
// underlying Money's Value.
func (n NullMoney) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Money.Value()
}
