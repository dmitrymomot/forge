// Package i18n provides internationalization support with immutable, thread-safe design
// and comprehensive locale handling for Go applications.
//
// This package offers zero-dependency internationalization with O(1) translation lookups,
// CLDR-compliant plural rules for 20+ languages, placeholder replacement, and robust
// language fallback mechanisms. All configuration is done at construction time, making
// instances immutable and safe for concurrent use.
//
// # Basic Usage
//
// Create an I18n instance with translations and retrieve localized text:
//
//	i18nInstance, err := i18n.New(
//		i18n.WithDefaultLanguage("en"),
//		i18n.WithTranslations("en", "app", map[string]any{
//			"welcome": "Welcome to our application",
//			"goodbye": "Goodbye, {{name}}!",
//		}),
//		i18n.WithTranslations("es", "app", map[string]any{
//			"welcome": "Bienvenido a nuestra aplicación",
//			"goodbye": "¡Adiós, {{name}}!",
//		}),
//	)
//
//	msg := i18nInstance.T("es", "app", "welcome")
//	// Output: "Bienvenido a nuestra aplicación"
//
//	farewell := i18nInstance.T("es", "app", "goodbye", i18n.M{"name": "Juan"})
//	// Output: "¡Adiós, Juan!"
//
// # File-Based Translations
//
// Load translations from JSON or YAML files using fs.FS:
//
//	//go:embed translations
//	var translationsFS embed.FS
//
//	subFS, _ := fs.Sub(translationsFS, "translations")
//	i18nInstance, err := i18n.New(
//		i18n.WithDefaultLanguage("en"),
//		i18n.WithJSONDir(subFS),
//		i18n.WithYAMLDir(subFS),
//	)
//
// File convention: {lang}/{namespace}.json (or .yaml/.yml)
//
// # Nested Translations
//
// Nested translation structures are automatically flattened for efficient
// lookups using dot notation:
//
//	i18nInstance, _ := i18n.New(
//		i18n.WithTranslations("en", "ui", map[string]any{
//			"buttons": map[string]any{
//				"save":   "Save",
//				"cancel": "Cancel",
//			},
//		}),
//	)
//
//	saveBtn := i18nInstance.T("en", "ui", "buttons.save")
//	// Output: "Save"
//
// # Pluralization
//
// Use Tn() for pluralized translations with CLDR-compliant plural rules:
//
//	i18nInstance, _ := i18n.New(
//		i18n.WithTranslations("en", "items", map[string]any{
//			"count": map[string]string{
//				"zero":  "No items",
//				"one":   "1 item",
//				"other": "{{count}} items",
//			},
//		}),
//	)
//
//	fmt.Println(i18nInstance.Tn("en", "items", "count", 0))  // "No items"
//	fmt.Println(i18nInstance.Tn("en", "items", "count", 1))  // "1 item"
//	fmt.Println(i18nInstance.Tn("en", "items", "count", 5))  // "5 items"
//
// # Language Fallback
//
// When a translation is not found in the requested language, the package
// automatically falls back to the default language, then to the key itself.
//
// # Translator
//
// The Translator type provides a simplified interface by fixing the language,
// namespace, and locale format:
//
//	translator := i18n.NewTranslator(i18nInstance, "de", "ui", i18n.FormatDeDE())
//
//	title := translator.T("page.title")
//	price := translator.FormatCurrency(19.99)  // "19,99 €"
//	date := translator.FormatDate(time.Now())   // "07.02.2026"
//
// # Predefined Locale Formats
//
// The package includes predefined formats for common locales:
//
//	i18n.FormatEnUS()  // $, MM/DD/YYYY, 12h
//	i18n.FormatEnGB()  // £, DD/MM/YYYY, 24h
//	i18n.FormatDeDE()  // € after, DD.MM.YYYY, 24h
//	i18n.FormatFrFR()  // € after, DD/MM/YYYY, 24h
//	i18n.FormatJaJP()  // ¥, YYYY/MM/DD, 24h
//
// # Accept-Language Header
//
// Parse HTTP Accept-Language headers to determine the best language match:
//
//	bestMatch := i18n.ParseAcceptLanguage("es-ES,es;q=0.9,en;q=0.8", available)
//
// # Thread Safety
//
// The I18n struct is immutable after creation, making it safe for concurrent use
// without additional synchronization. Translation lookups are O(1) through internal
// key flattening.
package i18n
