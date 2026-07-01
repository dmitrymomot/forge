package money

// Currency is ISO-4217 currency metadata.
type Currency struct {
	// Code is the ISO-4217 alphabetic code, e.g. "USD".
	Code string
	// Num is the ISO-4217 numeric code, e.g. "840".
	Num string
	// Symbol is a display symbol, e.g. "$". It may equal Code when there is no
	// distinct symbol.
	Symbol string
	// MinorUnits is the number of fractional digits, e.g. USD 2, JPY 0, BHD 3.
	MinorUnits int32
}
