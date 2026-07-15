package country

// ISO-3166-1 static data: alpha-2/alpha-3/numeric codes, English short name,
// primary official ISO-4217 currency, and E.164 dial code. Curated static data
// committed to the repo — no runtime fetch. Emoji flags are derived from the
// alpha-2 pair at package init. Source: ISO-3166-1 (2026 edition).

var (
	AU = Country{Alpha2: "AU", Alpha3: "AUS", Numeric: "036", Name: "Australia", Currency: "AUD", DialCode: "61"}
	BR = Country{Alpha2: "BR", Alpha3: "BRA", Numeric: "076", Name: "Brazil", Currency: "BRL", DialCode: "55"}
	CA = Country{Alpha2: "CA", Alpha3: "CAN", Numeric: "124", Name: "Canada", Currency: "CAD", DialCode: "1"}
	DE = Country{Alpha2: "DE", Alpha3: "DEU", Numeric: "276", Name: "Germany", Currency: "EUR", DialCode: "49"}
	FR = Country{Alpha2: "FR", Alpha3: "FRA", Numeric: "250", Name: "France", Currency: "EUR", DialCode: "33"}
	GB = Country{Alpha2: "GB", Alpha3: "GBR", Numeric: "826", Name: "United Kingdom", Currency: "GBP", DialCode: "44"}
	IN = Country{Alpha2: "IN", Alpha3: "IND", Numeric: "356", Name: "India", Currency: "INR", DialCode: "91"}
	JP = Country{Alpha2: "JP", Alpha3: "JPN", Numeric: "392", Name: "Japan", Currency: "JPY", DialCode: "81"}
	NO = Country{Alpha2: "NO", Alpha3: "NOR", Numeric: "578", Name: "Norway", Currency: "NOK", DialCode: "47"}
	UA = Country{Alpha2: "UA", Alpha3: "UKR", Numeric: "804", Name: "Ukraine", Currency: "UAH", DialCode: "380"}
	US = Country{Alpha2: "US", Alpha3: "USA", Numeric: "840", Name: "United States", Currency: "USD", DialCode: "1"}
	ZA = Country{Alpha2: "ZA", Alpha3: "ZAF", Numeric: "710", Name: "South Africa", Currency: "ZAR", DialCode: "27"}
)

// all is the bundled table as pointers into the exported vars, so the init emoji
// fill lands on the vars themselves. Extended to the full ISO-3166-1 set in a
// later task.
var all = []*Country{
	&AU, &BR, &CA, &DE, &FR, &GB, &IN, &JP, &NO, &UA, &US, &ZA,
}
