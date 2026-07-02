package money

import (
	"encoding/json"
	"fmt"

	"github.com/dmitrymomot/forge/decimal"
)

// moneyJSON is the JSON wire shape for Money: an exact decimal amount string and
// an ISO-4217 currency code.
type moneyJSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON implements json.Marshaler, emitting {"amount":"1.50","currency":
// "USD"}. The amount is the exact, full-precision decimal string (NOT rounded to
// minor units). A Money with no currency code cannot be represented and returns
// ErrScan (matching Value), so any value it does emit round-trips losslessly.
func (m Money) MarshalJSON() ([]byte, error) {
	if m.currency.Code == "" {
		return nil, fmt.Errorf("money: cannot marshal money with empty currency: %w", ErrScan)
	}
	return json.Marshal(moneyJSON{Amount: m.amount.String(), Currency: m.currency.Code})
}

// UnmarshalJSON implements json.Unmarshaler for the {"amount","currency"} shape.
// A JSON null leaves the receiver unchanged. The amount is parsed exactly; the
// currency code is resolved against the bundled ISO-4217 table and yields
// ErrUnknownCurrency if absent.
func (m *Money) UnmarshalJSON(p []byte) error {
	if string(p) == "null" {
		return nil
	}
	var raw moneyJSON
	if err := json.Unmarshal(p, &raw); err != nil {
		return err
	}
	amt, err := decimal.Parse(raw.Amount)
	if err != nil {
		return err
	}
	c, ok := CurrencyByCode(raw.Currency)
	if !ok {
		return ErrUnknownCurrency
	}
	m.amount, m.currency = amt, c
	return nil
}
