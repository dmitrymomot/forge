package validate

import "strings"

// currencyCodes holds the commonly-used subset of active ISO-4217 alpha-3
// codes accepted by CurrencyCode; it is not the full ISO-4217 list.
var currencyCodes = toSet(`USD EUR GBP JPY CHF CAD AUD NZD CNY HKD SGD SEK NOK DKK
PLN CZK HUF RON BGN TRY RUB UAH INR IDR MYR PHP THB VND KRW TWD ZAR NGN EGP MAD
KES GHS BRL MXN ARS CLP COP PEN AED SAR QAR KWD BHD OMR ILS`)

// countryCodes holds the commonly-used subset of ISO-3166-1 alpha-2 codes
// accepted by CountryCode; it is not the full ISO-3166-1 list.
var countryCodes = toSet(`US GB DE FR IT ES PT NL BE LU IE DK SE NO FI IS PL CZ SK
HU RO BG GR AT CH UA RU TR CA MX BR AR CL CO PE AU NZ JP CN HK TW KR SG MY TH VN
ID PH IN AE SA QA KW BH OM IL ZA NG EG MA KE GH`)

func toSet(s string) map[string]struct{} {
	fields := strings.Fields(s)
	m := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		m[f] = struct{}{}
	}
	return m
}
