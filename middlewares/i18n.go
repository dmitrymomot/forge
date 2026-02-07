package middlewares

import (
	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/i18n"
)

// I18nConfig configures the I18n middleware.
type I18nConfig struct {
	FormatMap     map[string]*i18n.LocaleFormat
	DefaultFormat *i18n.LocaleFormat
	Namespace     string
	Extractor     internal.Extractor
	extractorSet  bool
}

// I18nOption configures I18nConfig.
type I18nOption func(*I18nConfig)

// WithI18nNamespace sets the default namespace for the context translator.
func WithI18nNamespace(ns string) I18nOption {
	return func(cfg *I18nConfig) {
		cfg.Namespace = ns
	}
}

// WithI18nExtractor sets a custom language extractor chain.
func WithI18nExtractor(ext internal.Extractor) I18nOption {
	return func(cfg *I18nConfig) {
		cfg.Extractor = ext
		cfg.extractorSet = true
	}
}

// WithI18nFormatMap sets the language-to-format mapping.
func WithI18nFormatMap(m map[string]*i18n.LocaleFormat) I18nOption {
	return func(cfg *I18nConfig) {
		cfg.FormatMap = m
	}
}

// WithI18nDefaultFormat sets the fallback locale format.
func WithI18nDefaultFormat(f *i18n.LocaleFormat) I18nOption {
	return func(cfg *I18nConfig) {
		cfg.DefaultFormat = f
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
func I18n(svc *i18n.I18n, opts ...I18nOption) internal.Middleware {
	cfg := &I18nConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Default extractor: cookie → accept-language
	if !cfg.extractorSet {
		cfg.Extractor = internal.NewExtractor(
			internal.FromCookie("lang"),
			FromAcceptLanguage(svc.Languages()),
		)
	}

	if cfg.DefaultFormat == nil {
		cfg.DefaultFormat = i18n.FormatEnUS()
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			lang, ok := cfg.Extractor.Extract(c)
			if !ok || lang == "" {
				lang = svc.DefaultLanguage()
			}

			format := cfg.DefaultFormat
			if cfg.FormatMap != nil {
				if f, exists := cfg.FormatMap[lang]; exists {
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
