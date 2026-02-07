package i18n

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

// DefaultLang is the default language code used when no default language is specified.
const DefaultLang = "en"

// I18n provides internationalization support with translations and pluralization.
// It is immutable after creation, making it safe for concurrent use.
type I18n struct {
	// Flattened translations map for O(1) lookups.
	// Key format: "lang:namespace:key.path"
	translations map[string]string

	// Plural rules per language.
	pluralRules map[string]PluralRule

	// Optional handler called when a translation key is not found.
	// Useful for detecting untranslated keys during development or monitoring gaps in translations.
	missingKeyHandler func(lang, namespace, key string)

	// Default/fallback language.
	defaultLang string

	// Pre-computed list of available languages.
	languages []string
}

// Option configures the I18n instance during construction.
type Option func(*I18n) error

// New creates a new I18n instance with the given options.
// All configuration happens during construction, making the instance
// immutable and thread-safe from creation.
func New(opts ...Option) (*I18n, error) {
	i := &I18n{
		translations: make(map[string]string),
		pluralRules:  make(map[string]PluralRule),
		defaultLang:  DefaultLang,
	}

	for _, opt := range opts {
		if err := opt(i); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	if i.defaultLang == "" {
		return nil, ErrEmptyLanguage
	}

	i.languages = i.buildLanguagesList()

	return i, nil
}

// WithDefaultLanguage sets the default/fallback language.
func WithDefaultLanguage(lang string) Option {
	return func(i *I18n) error {
		if lang == "" {
			return ErrEmptyLanguage
		}
		i.defaultLang = lang
		return nil
	}
}

// WithLanguages sets the supported languages for the I18n instance.
// The default language will always be included and placed first in the list.
// Other languages will be sorted alphabetically.
func WithLanguages(langs ...string) Option {
	return func(i *I18n) error {
		if len(langs) == 0 {
			return nil
		}

		langSet := make(map[string]bool)
		for _, lang := range langs {
			if lang != "" {
				langSet[lang] = true
			}
		}

		i.languages = make([]string, 0, len(langSet)+1)
		i.languages = append(i.languages, i.defaultLang)

		delete(langSet, i.defaultLang)

		if len(langSet) > 0 {
			otherLangs := make([]string, 0, len(langSet))
			for lang := range langSet {
				otherLangs = append(otherLangs, lang)
			}
			sort.Strings(otherLangs)
			i.languages = append(i.languages, otherLangs...)
		}

		return nil
	}
}

// WithTranslations loads translations for a specific language and namespace.
// The translations map can be nested; it will be flattened internally for
// efficient lookups.
func WithTranslations(lang, namespace string, translations map[string]any) Option {
	return func(i *I18n) error {
		if lang == "" {
			return ErrEmptyLanguage
		}
		if namespace == "" {
			return ErrEmptyNamespace
		}
		if len(translations) == 0 {
			return nil
		}

		flattened := flattenTranslations(translations, "")

		for key, value := range flattened {
			compositeKey := buildKey(lang, namespace, key)
			i.translations[compositeKey] = value
		}

		if _, exists := i.pluralRules[lang]; !exists {
			i.pluralRules[lang] = GetPluralRuleForLanguage(lang)
		}

		return nil
	}
}

// WithPluralRule registers a custom plural rule for a language.
func WithPluralRule(lang string, rule PluralRule) Option {
	return func(i *I18n) error {
		if lang == "" {
			return ErrEmptyLanguage
		}
		if rule == nil {
			return ErrNilPluralRule
		}
		i.pluralRules[lang] = rule
		return nil
	}
}

// WithMissingKeyHandler sets a handler function that will be called when a translation
// key is not found in any language (including the default fallback).
func WithMissingKeyHandler(handler func(lang, namespace, key string)) Option {
	return func(i *I18n) error {
		i.missingKeyHandler = handler
		return nil
	}
}

// T retrieves a translation for the given language, namespace, and key.
// Placeholders in the translation are replaced with values from the provided maps.
// Falls back to the default language if translation is not found.
// Returns the key itself if no translation exists.
func (i *I18n) T(lang, namespace, key string, placeholders ...M) string {
	compositeKey := buildKey(lang, namespace, key)
	if translation, exists := i.translations[compositeKey]; exists {
		return replacePlaceholdersWithMerge(translation, placeholders...)
	}

	if base := baseLanguage(lang); base != lang {
		baseKey := buildKey(base, namespace, key)
		if translation, exists := i.translations[baseKey]; exists {
			return replacePlaceholdersWithMerge(translation, placeholders...)
		}
	}

	if lang != i.defaultLang && baseLanguage(lang) != i.defaultLang {
		defaultKey := buildKey(i.defaultLang, namespace, key)
		if translation, exists := i.translations[defaultKey]; exists {
			return replacePlaceholdersWithMerge(translation, placeholders...)
		}
	}

	if i.missingKeyHandler != nil {
		i.missingKeyHandler(lang, namespace, key)
	}

	return key
}

// Tn retrieves a pluralized translation for the given count.
// It automatically selects the appropriate plural form based on the language's plural rule
// and injects the count as a placeholder.
func (i *I18n) Tn(lang, namespace, key string, n int, placeholders ...M) string {
	rule, exists := i.pluralRules[lang]
	if !exists {
		if base := baseLanguage(lang); base != lang {
			rule, exists = i.pluralRules[base]
		}
		if !exists {
			if rule, exists = i.pluralRules[i.defaultLang]; !exists {
				rule = DefaultPluralRule
			}
		}
	}

	form := rule(n)
	pluralKey := key + "." + form

	var translation string
	var found bool

	// Try exact language
	found, translation = i.findPluralTranslation(lang, namespace, pluralKey, key, form)

	// Try base language (e.g., "en" for "en-US")
	if !found {
		if base := baseLanguage(lang); base != lang {
			found, translation = i.findPluralTranslation(base, namespace, pluralKey, key, form)
		}
	}

	// Try default language
	if !found && lang != i.defaultLang && baseLanguage(lang) != i.defaultLang {
		found, translation = i.findPluralTranslation(i.defaultLang, namespace, pluralKey, key, form)
	}

	if !found {
		if i.missingKeyHandler != nil {
			i.missingKeyHandler(lang, namespace, key)
		}
		return key
	}

	mergedPlaceholders := M{"count": n}
	for _, p := range placeholders {
		maps.Copy(mergedPlaceholders, p)
	}

	return ReplacePlaceholders(translation, mergedPlaceholders)
}

// findPluralTranslation tries to find a plural translation for a given language,
// checking the exact form first, then fallback forms.
func (i *I18n) findPluralTranslation(lang, namespace, pluralKey, key, form string) (bool, string) {
	compositeKey := buildKey(lang, namespace, pluralKey)
	if trans, exists := i.translations[compositeKey]; exists {
		return true, trans
	}
	for _, fallbackForm := range getPluralFallbackForms(form) {
		fallbackKey := buildKey(lang, namespace, key+"."+fallbackForm)
		if trans, exists := i.translations[fallbackKey]; exists {
			return true, trans
		}
	}
	return false, ""
}

// Languages returns the list of available languages.
func (i *I18n) Languages() []string {
	return i.languages
}

// DefaultLanguage returns the default/fallback language.
func (i *I18n) DefaultLanguage() string {
	return i.defaultLang
}

func (i *I18n) buildLanguagesList() []string {
	if len(i.languages) > 0 {
		return i.languages
	}
	return []string{i.defaultLang}
}

func buildKey(lang, namespace, key string) string {
	return lang + ":" + namespace + ":" + key
}

func flattenTranslations(data map[string]any, prefix string) map[string]string {
	result := make(map[string]string)

	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case string:
			result[fullKey] = v
		case map[string]any:
			nested := flattenTranslations(v, fullKey)
			maps.Copy(result, nested)
		case map[string]string:
			for subKey, subVal := range v {
				result[fullKey+"."+subKey] = subVal
			}
		default:
			result[fullKey] = fmt.Sprintf("%v", v)
		}
	}

	return result
}

func replacePlaceholdersWithMerge(template string, placeholders ...M) string {
	if len(placeholders) == 0 {
		return template
	}

	merged := make(M)
	for _, p := range placeholders {
		maps.Copy(merged, p)
	}

	return ReplacePlaceholders(template, merged)
}

// baseLanguage strips the region from a language tag (e.g., "en-US" → "en").
// Returns the input unchanged if there is no region.
func baseLanguage(lang string) string {
	if i := strings.IndexByte(lang, '-'); i > 0 {
		return lang[:i]
	}
	return lang
}

func getPluralFallbackForms(form string) []string {
	switch form {
	case PluralZero:
		return []string{PluralOther}
	case PluralOne:
		return []string{PluralOther}
	case PluralTwo:
		return []string{PluralFew, PluralMany, PluralOther}
	case PluralFew:
		return []string{PluralMany, PluralOther}
	case PluralMany:
		return []string{PluralOther}
	case PluralOther:
		return []string{}
	default:
		return []string{PluralOther}
	}
}
