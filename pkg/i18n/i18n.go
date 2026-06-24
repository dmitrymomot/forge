package i18n

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
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

	// Languages explicitly registered via WithLanguages.
	explicitLangs []string

	// Set of languages seen while loading translations
	// (WithTranslations/WithJSONDir/WithYAMLDir).
	loadedLangs map[string]struct{}

	// Pre-computed list of available languages.
	languages []string
}

// Config holds i18n configuration.
type Config struct {
	DefaultLanguage string `env:"DEFAULT_LANGUAGE" envDefault:"en"`
}

// Option configures the I18n instance during construction.
type Option func(*I18n) error

// New creates a new I18n instance with the given config and options.
// All configuration happens during construction, making the instance
// immutable and thread-safe from creation.
func New(cfg Config, opts ...Option) (*I18n, error) {
	if cfg.DefaultLanguage == "" {
		cfg.DefaultLanguage = DefaultLang
	}

	i := &I18n{
		translations: make(map[string]string),
		pluralRules:  make(map[string]PluralRule),
		defaultLang:  cfg.DefaultLanguage,
		loadedLangs:  make(map[string]struct{}),
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

// WithLanguages sets the supported languages for the I18n instance.
// The default language will always be included and placed first in the list.
// Other languages will be sorted alphabetically. Languages discovered while
// loading translations (WithTranslations/WithJSONDir/WithYAMLDir) are also
// included automatically.
func WithLanguages(langs ...string) Option {
	return func(i *I18n) error {
		for _, lang := range langs {
			if lang != "" {
				i.explicitLangs = append(i.explicitLangs, lang)
			}
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

		flattened, err := flattenTranslations(translations, "")
		if err != nil {
			return fmt.Errorf("%w: %s/%s: %w", ErrInvalidFile, lang, namespace, err)
		}

		for key, value := range flattened {
			compositeKey := buildKey(lang, namespace, key)
			i.translations[compositeKey] = value
		}

		i.loadedLangs[lang] = struct{}{}

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

// Languages returns the list of available languages. The returned slice is a
// copy; mutating it does not affect the instance's internal state.
func (i *I18n) Languages() []string {
	return slices.Clone(i.languages)
}

// DefaultLanguage returns the default/fallback language.
func (i *I18n) DefaultLanguage() string {
	return i.defaultLang
}

// buildLanguagesList computes the available-languages list from the default
// language, languages registered via WithLanguages, and languages discovered
// while loading translations. The default language is always first; the rest
// are unique and sorted alphabetically.
func (i *I18n) buildLanguagesList() []string {
	others := make(map[string]struct{})

	for _, lang := range i.explicitLangs {
		if lang != i.defaultLang {
			others[lang] = struct{}{}
		}
	}
	for lang := range i.loadedLangs {
		if lang != i.defaultLang {
			others[lang] = struct{}{}
		}
	}

	langs := make([]string, 0, len(others)+1)
	langs = append(langs, i.defaultLang)

	if len(others) > 0 {
		otherLangs := make([]string, 0, len(others))
		for lang := range others {
			otherLangs = append(otherLangs, lang)
		}
		slices.Sort(otherLangs)
		langs = append(langs, otherLangs...)
	}

	return langs
}

func buildKey(lang, namespace, key string) string {
	return lang + ":" + namespace + ":" + key
}

// flattenTranslations flattens a nested translation map into dot-separated keys.
// String, boolean, and numeric scalars are supported. Nested maps recurse.
// Any other type (slices, nil, functions, etc.) is rejected with an error rather
// than being silently stringified. Duplicate keys are also rejected so that a
// scalar and a nested branch cannot silently overwrite one another.
func flattenTranslations(data map[string]any, prefix string) (map[string]string, error) {
	result := make(map[string]string)

	set := func(key, value string) error {
		if _, exists := result[key]; exists {
			return fmt.Errorf("duplicate translation key %q", key)
		}
		result[key] = value
		return nil
	}

	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case string:
			if err := set(fullKey, v); err != nil {
				return nil, err
			}
		case bool:
			if err := set(fullKey, strconv.FormatBool(v)); err != nil {
				return nil, err
			}
		case int:
			if err := set(fullKey, strconv.Itoa(v)); err != nil {
				return nil, err
			}
		case int64:
			if err := set(fullKey, strconv.FormatInt(v, 10)); err != nil {
				return nil, err
			}
		case float64:
			if err := set(fullKey, strconv.FormatFloat(v, 'g', -1, 64)); err != nil {
				return nil, err
			}
		case map[string]any:
			nested, err := flattenTranslations(v, fullKey)
			if err != nil {
				return nil, err
			}
			for k, val := range nested {
				if err := set(k, val); err != nil {
					return nil, err
				}
			}
		case map[string]string:
			for subKey, subVal := range v {
				if err := set(fullKey+"."+subKey, subVal); err != nil {
					return nil, err
				}
			}
		default:
			return nil, fmt.Errorf("unsupported translation value type %T for key %q", value, fullKey)
		}
	}

	return result, nil
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
