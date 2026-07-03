package money

import "strings"

// currencyIndex maps an uppercase ISO-4217 code to its Currency. Built once from
// allCurrencies at init.
var currencyIndex = buildCurrencyIndex()

func buildCurrencyIndex() map[string]Currency {
	m := make(map[string]Currency, len(allCurrencies))
	for _, c := range allCurrencies {
		m[c.Code] = c
	}
	return m
}

// CurrencyByCode looks up a currency by ISO-4217 alphabetic code. The lookup is
// case-insensitive. It returns the currency and true when found, or the zero
// Currency and false otherwise.
func CurrencyByCode(code string) (Currency, bool) {
	c, ok := currencyIndex[strings.ToUpper(code)]
	return c, ok
}
