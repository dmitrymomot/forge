package i18n

// FormatEnUS returns a LocaleFormat configured for US English (en-US).
func FormatEnUS() *LocaleFormat {
	return NewLocaleFormat()
}

// FormatEnGB returns a LocaleFormat configured for British English (en-GB).
func FormatEnGB() *LocaleFormat {
	return NewLocaleFormat(
		WithCurrencySymbol("\u00a3"),
		WithDateFormat("02/01/2006"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("02/01/2006 15:04"),
	)
}

// FormatDeDE returns a LocaleFormat configured for German (de-DE).
func FormatDeDE() *LocaleFormat {
	return NewLocaleFormat(
		WithDecimalSeparator(","),
		WithThousandSeparator("."),
		WithCurrencySymbol("\u20ac"),
		WithCurrencyPosition("after"),
		WithDateFormat("02.01.2006"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("02.01.2006 15:04"),
	)
}

// FormatFrFR returns a LocaleFormat configured for French (fr-FR).
func FormatFrFR() *LocaleFormat {
	return NewLocaleFormat(
		WithDecimalSeparator(","),
		WithThousandSeparator(" "),
		WithCurrencySymbol("\u20ac"),
		WithCurrencyPosition("after"),
		WithDateFormat("02/01/2006"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("02/01/2006 15:04"),
	)
}

// FormatEsES returns a LocaleFormat configured for Spanish (es-ES).
func FormatEsES() *LocaleFormat {
	return NewLocaleFormat(
		WithDecimalSeparator(","),
		WithThousandSeparator("."),
		WithCurrencySymbol("\u20ac"),
		WithCurrencyPosition("after"),
		WithDateFormat("02/01/2006"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("02/01/2006 15:04"),
	)
}

// FormatPtBR returns a LocaleFormat configured for Brazilian Portuguese (pt-BR).
func FormatPtBR() *LocaleFormat {
	return NewLocaleFormat(
		WithDecimalSeparator(","),
		WithThousandSeparator("."),
		WithCurrencySymbol("R$"),
		WithDateFormat("02/01/2006"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("02/01/2006 15:04"),
	)
}

// FormatJaJP returns a LocaleFormat configured for Japanese (ja-JP).
func FormatJaJP() *LocaleFormat {
	return NewLocaleFormat(
		WithCurrencySymbol("\u00a5"),
		WithDateFormat("2006/01/02"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("2006/01/02 15:04"),
	)
}

// FormatZhCN returns a LocaleFormat configured for Simplified Chinese (zh-CN).
func FormatZhCN() *LocaleFormat {
	return NewLocaleFormat(
		WithCurrencySymbol("\u00a5"),
		WithDateFormat("2006-01-02"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("2006-01-02 15:04"),
	)
}

// FormatKoKR returns a LocaleFormat configured for Korean (ko-KR).
func FormatKoKR() *LocaleFormat {
	return NewLocaleFormat(
		WithCurrencySymbol("\u20a9"),
		WithDateFormat("2006.01.02"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("2006.01.02 15:04"),
	)
}

// FormatPlPL returns a LocaleFormat configured for Polish (pl-PL).
func FormatPlPL() *LocaleFormat {
	return NewLocaleFormat(
		WithDecimalSeparator(","),
		WithThousandSeparator(" "),
		WithCurrencySymbol("z\u0142"),
		WithCurrencyPosition("after"),
		WithDateFormat("02.01.2006"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("02.01.2006 15:04"),
	)
}

// FormatRuRU returns a LocaleFormat configured for Russian (ru-RU).
func FormatRuRU() *LocaleFormat {
	return NewLocaleFormat(
		WithDecimalSeparator(","),
		WithThousandSeparator(" "),
		WithCurrencySymbol("\u20bd"),
		WithCurrencyPosition("after"),
		WithDateFormat("02.01.2006"),
		WithTimeFormat("15:04"),
		WithDateTimeFormat("02.01.2006 15:04"),
	)
}

// FormatArSA returns a LocaleFormat configured for Arabic (ar-SA).
func FormatArSA() *LocaleFormat {
	return NewLocaleFormat(
		WithCurrencySymbol("SAR"),
		WithCurrencyPosition("after"),
		WithDateFormat("02/01/2006"),
		WithTimeFormat("3:04 PM"),
		WithDateTimeFormat("02/01/2006 3:04 PM"),
	)
}
