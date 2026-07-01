package money

// ISO-4217 currency table. Generated static data: alphabetic Code, numeric Num,
// MinorUnits (fractional digits), and a display Symbol (Symbol == Code when no
// distinct glyph exists). Source of truth for CurrencyByCode and the package
// vars below.

// Exported currency vars — the common set callers reference directly.
var (
	AED = Currency{Code: "AED", Num: "784", Symbol: "د.إ", MinorUnits: 2}
	AUD = Currency{Code: "AUD", Num: "036", Symbol: "$", MinorUnits: 2}
	BHD = Currency{Code: "BHD", Num: "048", Symbol: ".د.ب", MinorUnits: 3}
	BRL = Currency{Code: "BRL", Num: "986", Symbol: "R$", MinorUnits: 2}
	CAD = Currency{Code: "CAD", Num: "124", Symbol: "$", MinorUnits: 2}
	CHF = Currency{Code: "CHF", Num: "756", Symbol: "CHF", MinorUnits: 2}
	CLF = Currency{Code: "CLF", Num: "990", Symbol: "UF", MinorUnits: 4}
	CNY = Currency{Code: "CNY", Num: "156", Symbol: "¥", MinorUnits: 2}
	CZK = Currency{Code: "CZK", Num: "203", Symbol: "Kč", MinorUnits: 2}
	DKK = Currency{Code: "DKK", Num: "208", Symbol: "kr", MinorUnits: 2}
	EUR = Currency{Code: "EUR", Num: "978", Symbol: "€", MinorUnits: 2}
	GBP = Currency{Code: "GBP", Num: "826", Symbol: "£", MinorUnits: 2}
	HKD = Currency{Code: "HKD", Num: "344", Symbol: "$", MinorUnits: 2}
	HUF = Currency{Code: "HUF", Num: "348", Symbol: "Ft", MinorUnits: 2}
	IDR = Currency{Code: "IDR", Num: "360", Symbol: "Rp", MinorUnits: 2}
	ILS = Currency{Code: "ILS", Num: "376", Symbol: "₪", MinorUnits: 2}
	INR = Currency{Code: "INR", Num: "356", Symbol: "₹", MinorUnits: 2}
	JPY = Currency{Code: "JPY", Num: "392", Symbol: "¥", MinorUnits: 0}
	KRW = Currency{Code: "KRW", Num: "410", Symbol: "₩", MinorUnits: 0}
	KWD = Currency{Code: "KWD", Num: "414", Symbol: "د.ك", MinorUnits: 3}
	MXN = Currency{Code: "MXN", Num: "484", Symbol: "$", MinorUnits: 2}
	MYR = Currency{Code: "MYR", Num: "458", Symbol: "RM", MinorUnits: 2}
	NOK = Currency{Code: "NOK", Num: "578", Symbol: "kr", MinorUnits: 2}
	NZD = Currency{Code: "NZD", Num: "554", Symbol: "$", MinorUnits: 2}
	PLN = Currency{Code: "PLN", Num: "985", Symbol: "zł", MinorUnits: 2}
	RUB = Currency{Code: "RUB", Num: "643", Symbol: "₽", MinorUnits: 2}
	SAR = Currency{Code: "SAR", Num: "682", Symbol: "﷼", MinorUnits: 2}
	SEK = Currency{Code: "SEK", Num: "752", Symbol: "kr", MinorUnits: 2}
	SGD = Currency{Code: "SGD", Num: "702", Symbol: "$", MinorUnits: 2}
	THB = Currency{Code: "THB", Num: "764", Symbol: "฿", MinorUnits: 2}
	TRY = Currency{Code: "TRY", Num: "949", Symbol: "₺", MinorUnits: 2}
	TWD = Currency{Code: "TWD", Num: "901", Symbol: "NT$", MinorUnits: 2}
	UAH = Currency{Code: "UAH", Num: "980", Symbol: "₴", MinorUnits: 2}
	USD = Currency{Code: "USD", Num: "840", Symbol: "$", MinorUnits: 2}
	VND = Currency{Code: "VND", Num: "704", Symbol: "₫", MinorUnits: 0}
	ZAR = Currency{Code: "ZAR", Num: "710", Symbol: "R", MinorUnits: 2}
)

// allCurrencies is the full bundled table indexed by CurrencyByCode. Entries not
// bound to an exported var are declared inline here.
var allCurrencies = []Currency{
	AED, AUD, BHD, BRL, CAD, CHF, CLF, CNY, CZK, DKK, EUR, GBP, HKD, HUF, IDR,
	ILS, INR, JPY, KRW, KWD, MXN, MYR, NOK, NZD, PLN, RUB, SAR, SEK, SGD, THB,
	TRY, TWD, UAH, USD, VND, ZAR,
	// Additional ISO-4217 entries without a dedicated exported var:
	{Code: "ARS", Num: "032", Symbol: "$", MinorUnits: 2},
	{Code: "BGN", Num: "975", Symbol: "лв", MinorUnits: 2},
	{Code: "COP", Num: "170", Symbol: "$", MinorUnits: 2},
	{Code: "EGP", Num: "818", Symbol: "£", MinorUnits: 2},
	{Code: "ISK", Num: "352", Symbol: "kr", MinorUnits: 0},
	{Code: "JOD", Num: "400", Symbol: "د.ا", MinorUnits: 3},
	{Code: "NGN", Num: "566", Symbol: "₦", MinorUnits: 2},
	{Code: "OMR", Num: "512", Symbol: "ر.ع.", MinorUnits: 3},
	{Code: "PHP", Num: "608", Symbol: "₱", MinorUnits: 2},
	{Code: "PKR", Num: "586", Symbol: "₨", MinorUnits: 2},
	{Code: "RON", Num: "946", Symbol: "lei", MinorUnits: 2},
	{Code: "TND", Num: "788", Symbol: "د.ت", MinorUnits: 3},
}
