package i18n

import "time"

// Translator provides a simplified translation interface with a fixed language and namespace context.
// It wraps an I18n instance and eliminates the need to specify language and namespace for each translation.
type Translator struct {
	i18n      *I18n
	format    *LocaleFormat
	language  string
	namespace string
}

// NewTranslator creates a new Translator with the specified language, namespace, and optional format.
// If format is nil, it defaults to FormatEnUS().
// If language is empty, it defaults to the I18n instance's default language.
func NewTranslator(i18n *I18n, language, namespace string, format *LocaleFormat) *Translator {
	if i18n == nil {
		panic("i18n: service is not provided")
	}
	if language == "" {
		language = i18n.DefaultLanguage()
	}
	if format == nil {
		format = FormatEnUS()
	}
	return &Translator{
		i18n:      i18n,
		language:  language,
		namespace: namespace,
		format:    format,
	}
}

// T translates a key using the translator's language and namespace context.
func (t *Translator) T(key string, placeholders ...M) string {
	return t.i18n.T(t.language, t.namespace, key, placeholders...)
}

// TranslateMessage translates a key with a single placeholder map.
// Its signature matches validator.TranslateFunc, allowing direct use as:
//
//	ve.Translate(translator.TranslateMessage)
func (t *Translator) TranslateMessage(key string, values map[string]any) string {
	return t.i18n.T(t.language, t.namespace, key, values)
}

// Tn translates a key with pluralization using the translator's language and namespace context.
func (t *Translator) Tn(key string, n int, placeholders ...M) string {
	return t.i18n.Tn(t.language, t.namespace, key, n, placeholders...)
}

// FormatNumber formats a number with locale-specific separators.
func (t *Translator) FormatNumber(n float64) string {
	return t.format.FormatNumber(n)
}

// FormatCurrency formats a currency amount with locale-specific formatting.
func (t *Translator) FormatCurrency(amount float64) string {
	return t.format.FormatCurrency(amount)
}

// FormatPercent formats a percentage with locale-specific formatting.
// The input should be a decimal (0.5 for 50%).
func (t *Translator) FormatPercent(n float64) string {
	return t.format.FormatPercent(n)
}

// FormatDate formats a date with locale-specific formatting.
func (t *Translator) FormatDate(date time.Time) string {
	return t.format.FormatDate(date)
}

// FormatTime formats a time with locale-specific formatting.
func (t *Translator) FormatTime(tm time.Time) string {
	return t.format.FormatTime(tm)
}

// FormatDateTime formats a datetime with locale-specific formatting.
func (t *Translator) FormatDateTime(datetime time.Time) string {
	return t.format.FormatDateTime(datetime)
}

// Language returns the translator's language.
func (t *Translator) Language() string {
	return t.language
}

// Namespace returns the translator's namespace.
func (t *Translator) Namespace() string {
	return t.namespace
}

// Format returns the LocaleFormat used by this translator.
func (t *Translator) Format() *LocaleFormat {
	return t.format
}
