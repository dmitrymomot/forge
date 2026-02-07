package i18n

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// LocaleFormat contains formatting rules and methods for locale-specific formatting.
// It is immutable after creation and safe for concurrent use.
type LocaleFormat struct {
	decimalSeparator  string
	thousandSeparator string
	currencySymbol    string
	currencyPosition  string // "before" or "after"
	percentSymbol     string
	dateFormat        string
	timeFormat        string
	dateTimeFormat    string
}

// LocaleFormatOption configures a LocaleFormat during construction.
type LocaleFormatOption func(*LocaleFormat)

// NewLocaleFormat creates a new LocaleFormat with the given options.
// If no options are provided, it defaults to US English formatting.
func NewLocaleFormat(opts ...LocaleFormatOption) *LocaleFormat {
	lf := &LocaleFormat{
		decimalSeparator:  ".",
		thousandSeparator: ",",
		currencySymbol:    "$",
		currencyPosition:  "before",
		percentSymbol:     "%",
		dateFormat:        "01/02/2006",
		timeFormat:        "3:04 PM",
		dateTimeFormat:    "01/02/2006 3:04 PM",
	}

	for _, opt := range opts {
		opt(lf)
	}

	return lf
}

// WithDecimalSeparator sets the decimal separator character.
func WithDecimalSeparator(sep string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		lf.decimalSeparator = sep
	}
}

// WithThousandSeparator sets the thousand separator character.
func WithThousandSeparator(sep string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		lf.thousandSeparator = sep
	}
}

// WithCurrencySymbol sets the currency symbol.
func WithCurrencySymbol(symbol string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		lf.currencySymbol = symbol
	}
}

// WithCurrencyPosition sets the currency position ("before" or "after").
func WithCurrencyPosition(pos string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		if pos == "before" || pos == "after" {
			lf.currencyPosition = pos
		}
	}
}

// WithPercentSymbol sets the percent symbol.
func WithPercentSymbol(symbol string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		lf.percentSymbol = symbol
	}
}

// WithDateFormat sets the date format string (Go time layout).
func WithDateFormat(format string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		lf.dateFormat = format
	}
}

// WithTimeFormat sets the time format string (Go time layout).
func WithTimeFormat(format string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		lf.timeFormat = format
	}
}

// WithDateTimeFormat sets the datetime format string (Go time layout).
func WithDateTimeFormat(format string) LocaleFormatOption {
	return func(lf *LocaleFormat) {
		lf.dateTimeFormat = format
	}
}

// FormatNumber formats a number with the locale's separators.
func (lf *LocaleFormat) FormatNumber(n float64) string {
	negative := n < 0
	if negative {
		n = -n
	}

	intPart := int64(n)
	decPart := n - float64(intPart)

	intStr := lf.formatIntegerWithSeparator(intPart)

	result := intStr
	if decPart > 0 {
		decPart = math.Round(decPart*100) / 100
		decStr := fmt.Sprintf("%.2f", decPart)[2:]
		decStr = strings.TrimRight(decStr, "0")
		if decStr != "" {
			result = intStr + lf.decimalSeparator + decStr
		}
	}

	if negative {
		result = "-" + result
	}

	return result
}

// FormatCurrency formats a currency amount with the locale's formatting.
func (lf *LocaleFormat) FormatCurrency(amount float64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}

	numStr := lf.formatCurrencyNumber(amount)

	var result string
	if lf.currencyPosition == "before" {
		if lf.currencySymbol == "$" || strings.HasSuffix(lf.currencySymbol, "$") || lf.currencySymbol == "\u00a5" || lf.currencySymbol == "\u00a3" || lf.currencySymbol == "\u20a9" {
			result = lf.currencySymbol + numStr
		} else {
			result = lf.currencySymbol + " " + numStr
		}
	} else {
		result = numStr + " " + lf.currencySymbol
	}

	if negative {
		result = "-" + result
	}

	return result
}

// FormatPercent formats a percentage with the locale's formatting.
func (lf *LocaleFormat) FormatPercent(n float64) string {
	percentage := n * 100
	numStr := lf.formatPercentNumber(percentage)
	return numStr + lf.percentSymbol
}

// FormatDate formats a date with the locale's date format.
func (lf *LocaleFormat) FormatDate(t time.Time) string {
	return t.Format(lf.dateFormat)
}

// FormatTime formats a time with the locale's time format.
func (lf *LocaleFormat) FormatTime(t time.Time) string {
	return t.Format(lf.timeFormat)
}

// FormatDateTime formats a datetime with the locale's datetime format.
func (lf *LocaleFormat) FormatDateTime(t time.Time) string {
	return t.Format(lf.dateTimeFormat)
}

func (lf *LocaleFormat) formatIntegerWithSeparator(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	str := fmt.Sprintf("%d", n)
	var result []string

	for i := len(str); i > 0; i -= 3 {
		start := max(0, i-3)
		result = append([]string{str[start:i]}, result...)
	}

	return strings.Join(result, lf.thousandSeparator)
}

func (lf *LocaleFormat) formatCurrencyNumber(n float64) string {
	n = math.Round(n*100) / 100

	intPart := int64(n)
	decPart := n - float64(intPart)

	intStr := lf.formatIntegerWithSeparator(intPart)

	decStr := fmt.Sprintf("%.2f", decPart)[2:]
	return intStr + lf.decimalSeparator + decStr
}

func (lf *LocaleFormat) formatPercentNumber(n float64) string {
	negative := n < 0
	if negative {
		n = -n
	}

	n = math.Round(n*10) / 10

	intPart := int64(n)
	decPart := n - float64(intPart)

	intStr := fmt.Sprintf("%d", intPart)

	result := intStr
	if decPart > 0 {
		decStr := fmt.Sprintf("%.1f", decPart)[2:]
		decStr = strings.TrimRight(decStr, "0")
		if decStr != "" {
			result = intStr + lf.decimalSeparator + decStr
		}
	}

	if negative {
		result = "-" + result
	}

	return result
}
