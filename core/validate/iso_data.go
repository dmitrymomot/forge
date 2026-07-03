package validate

import "strings"

// currencyCodes is the active ISO-4217 alpha-3 set. Seeded with common codes;
// append the remaining active codes before merge (data entry only).
var currencyCodes = toSet(`USD EUR GBP JPY CHF CAD AUD NZD CNY HKD SGD SEK NOK DKK
PLN CZK HUF RON BGN TRY RUB UAH INR IDR MYR PHP THB VND KRW TWD ZAR NGN EGP MAD
KES GHS BRL MXN ARS CLP COP PEN AED SAR QAR KWD BHD OMR ILS`)

// countryCodes is the ISO-3166-1 alpha-2 set. Seeded with common codes;
// append the remaining codes before merge (data entry only).
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
