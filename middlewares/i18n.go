package middlewares

import (
	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/i18n"
)

// I18nConfig configures the I18n middleware.
type I18nConfig struct {
	Namespace string `env:"NAMESPACE"`
}

type i18nOptions struct {
	formatMap     map[string]*i18n.LocaleFormat
	defaultFormat *i18n.LocaleFormat
	extractor     internal.Extractor
	extractorSet  bool
}

// I18nOption configures runtime dependencies for the I18n middleware.
type I18nOption func(*i18nOptions)

// WithI18nExtractor sets a custom language extractor chain.
func WithI18nExtractor(ext internal.Extractor) I18nOption {
	return func(o *i18nOptions) {
		o.extractor = ext
		o.extractorSet = true
	}
}

// WithI18nFormatMap sets the language-to-format mapping.
func WithI18nFormatMap(m map[string]*i18n.LocaleFormat) I18nOption {
	return func(o *i18nOptions) {
		o.formatMap = m
	}
}

// WithI18nDefaultFormat sets the fallback locale format.
func WithI18nDefaultFormat(f *i18n.LocaleFormat) I18nOption {
	return func(o *i18nOptions) {
		o.defaultFormat = f
	}
}

// FromAcceptLanguage returns an ExtractorSource that parses the Accept-Language
// header and matches against the available languages.
func FromAcceptLanguage(available []string) internal.ExtractorSource {
	return func(c internal.Context) (string, bool) {
		header := c.Header("Accept-Language")
		if header == "" {
			return "", false
		}
		lang := i18n.ParseAcceptLanguage(header, available)
		return lang, true
	}
}

// I18n returns middleware that resolves the user's language, creates a Translator,
// and stores both in the request context.
func I18n(svc *i18n.I18n, cfg I18nConfig, opts ...I18nOption) internal.Middleware {
	o := &i18nOptions{}
	for _, opt := range opts {
		opt(o)
	}

	// Default extractor: cookie -> accept-language
	if !o.extractorSet {
		o.extractor = internal.NewExtractor(
			internal.FromCookie("lang"),
			FromAcceptLanguage(svc.Languages()),
		)
	}

	if o.defaultFormat == nil {
		o.defaultFormat = i18n.FormatEnUS()
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			lang, ok := o.extractor.Extract(c)
			if !ok || lang == "" {
				lang = svc.DefaultLanguage()
			}

			format := o.defaultFormat
			if o.formatMap != nil {
				if f, exists := o.formatMap[lang]; exists {
					format = f
				}
			}

			tr := i18n.NewTranslator(svc, lang, cfg.Namespace, format)

			c.Set(internal.TranslatorKey{}, tr)
			c.Set(internal.LanguageKey{}, lang)

			return next(c)
		}
	}
}

// GetTranslator extracts the Translator from the context.
// Returns nil if the I18n middleware is not used.
func GetTranslator(c internal.Context) *i18n.Translator {
	if v, ok := c.Get(internal.TranslatorKey{}).(*i18n.Translator); ok {
		return v
	}
	return nil
}

// GetLanguage extracts the resolved language from the context.
// Returns an empty string if the I18n middleware is not used.
func GetLanguage(c internal.Context) string {
	if v, ok := c.Get(internal.LanguageKey{}).(string); ok {
		return v
	}
	return ""
}
