package i18n_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates instance with defaults", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New()
		require.NoError(t, err)
		require.NotNil(t, inst)
		require.Equal(t, "en", inst.DefaultLanguage())
	})

	t.Run("sets custom default language", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(i18n.WithDefaultLanguage("pl"))
		require.NoError(t, err)
		require.Equal(t, "pl", inst.DefaultLanguage())
	})

	t.Run("returns error for empty default language", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(i18n.WithDefaultLanguage(""))
		require.Error(t, err)
		require.ErrorIs(t, err, i18n.ErrEmptyLanguage)
	})

	t.Run("loads translations", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithTranslations("en", "general", map[string]any{
				"hello": "Hello",
			}),
		)
		require.NoError(t, err)
		require.NotNil(t, inst)
	})

	t.Run("returns error for empty language in translations", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(
			i18n.WithTranslations("", "general", map[string]any{"hello": "Hello"}),
		)
		require.Error(t, err)
		require.ErrorIs(t, err, i18n.ErrEmptyLanguage)
	})

	t.Run("returns error for empty namespace in translations", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(
			i18n.WithTranslations("en", "", map[string]any{"hello": "Hello"}),
		)
		require.Error(t, err)
		require.ErrorIs(t, err, i18n.ErrEmptyNamespace)
	})

	t.Run("allows empty translations map", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithTranslations("en", "general", map[string]any{}),
		)
		require.NoError(t, err)
		require.NotNil(t, inst)
	})

	t.Run("sets custom plural rule", func(t *testing.T) {
		t.Parallel()
		customRule := func(n int) string {
			if n == 1 {
				return "one"
			}
			return "other"
		}
		inst, err := i18n.New(i18n.WithPluralRule("en", customRule))
		require.NoError(t, err)
		require.NotNil(t, inst)
	})

	t.Run("returns error for nil plural rule", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(i18n.WithPluralRule("en", nil))
		require.Error(t, err)
		require.ErrorIs(t, err, i18n.ErrNilPluralRule)
	})

	t.Run("sets missing key handler", func(t *testing.T) {
		t.Parallel()
		var missingKeys []string
		handler := func(lang, namespace, key string) {
			missingKeys = append(missingKeys, fmt.Sprintf("%s:%s:%s", lang, namespace, key))
		}

		inst, err := i18n.New(
			i18n.WithMissingKeyHandler(handler),
			i18n.WithTranslations("en", "test", map[string]any{
				"existing": "Exists",
			}),
		)
		require.NoError(t, err)

		result := inst.T("en", "test", "existing")
		require.Equal(t, "Exists", result)
		require.Empty(t, missingKeys)

		result = inst.T("en", "test", "missing")
		require.Equal(t, "missing", result)
		require.Equal(t, []string{"en:test:missing"}, missingKeys)
	})

	t.Run("sets languages list", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithLanguages("en", "pl", "de"),
		)
		require.NoError(t, err)
		require.Equal(t, []string{"en", "de", "pl"}, inst.Languages())
	})
}

func TestT(t *testing.T) {
	t.Parallel()

	setup := func() *i18n.I18n {
		inst, _ := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithTranslations("en", "general", map[string]any{
				"hello":   "Hello",
				"welcome": "Welcome, {{name}}!",
				"goodbye": "Goodbye, {{name}}! See you {{when}}.",
				"errors": map[string]any{
					"not_found": "Resource not found",
					"validation": map[string]any{
						"required": "Field {{field}} is required",
						"email":    "Invalid email format",
					},
				},
			}),
			i18n.WithTranslations("pl", "general", map[string]any{
				"hello":   "Cze\u015b\u0107",
				"welcome": "Witaj, {{name}}!",
				"errors": map[string]any{
					"not_found": "Zas\u00f3b nie znaleziony",
				},
			}),
		)
		return inst
	}

	t.Run("returns simple translation", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "Hello", inst.T("en", "general", "hello"))
	})

	t.Run("returns translation with placeholder", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.T("en", "general", "welcome", i18n.M{"name": "John"})
		require.Equal(t, "Welcome, John!", result)
	})

	t.Run("returns translation with multiple placeholders", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.T("en", "general", "goodbye", i18n.M{
			"name": "Alice",
			"when": "tomorrow",
		})
		require.Equal(t, "Goodbye, Alice! See you tomorrow.", result)
	})

	t.Run("merges multiple placeholder maps", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.T("en", "general", "goodbye",
			i18n.M{"name": "Bob"},
			i18n.M{"when": "later"},
		)
		require.Equal(t, "Goodbye, Bob! See you later.", result)
	})

	t.Run("later placeholder maps override earlier ones", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.T("en", "general", "welcome",
			i18n.M{"name": "Initial"},
			i18n.M{"name": "Override"},
		)
		require.Equal(t, "Welcome, Override!", result)
	})

	t.Run("returns nested translation using dot notation", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "Resource not found", inst.T("en", "general", "errors.not_found"))
	})

	t.Run("returns deeply nested translation", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "Invalid email format", inst.T("en", "general", "errors.validation.email"))
	})

	t.Run("returns nested translation with placeholder", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.T("en", "general", "errors.validation.required", i18n.M{"field": "username"})
		require.Equal(t, "Field username is required", result)
	})

	t.Run("falls back to default language", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.T("pl", "general", "goodbye", i18n.M{
			"name": "Anna",
			"when": "jutro",
		})
		require.Equal(t, "Goodbye, Anna! See you jutro.", result)
	})

	t.Run("returns key when translation not found", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "non.existent.key", inst.T("en", "general", "non.existent.key"))
	})

	t.Run("returns key when namespace not found", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "hello", inst.T("en", "nonexistent", "hello"))
	})

	t.Run("leaves unmatched placeholders unchanged", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.T("en", "general", "welcome", i18n.M{"other": "value"})
		require.Equal(t, "Welcome, {{name}}!", result)
	})

	t.Run("handles empty placeholder maps", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "Welcome, {{name}}!", inst.T("en", "general", "welcome"))
	})
}

func TestTn(t *testing.T) {
	t.Parallel()

	setup := func() *i18n.I18n {
		inst, _ := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithPluralRule("en", i18n.EnglishPluralRule),
			i18n.WithPluralRule("pl", i18n.SlavicPluralRule),
			i18n.WithTranslations("en", "general", map[string]any{
				"items": map[string]any{
					"zero":  "No items",
					"one":   "{{count}} item",
					"other": "{{count}} items",
				},
				"messages": map[string]any{
					"one":   "You have {{count}} new message",
					"other": "You have {{count}} new messages",
				},
			}),
			i18n.WithTranslations("pl", "general", map[string]any{
				"items": map[string]any{
					"zero": "Brak element\u00f3w",
					"one":  "{{count}} element",
					"few":  "{{count}} elementy",
					"many": "{{count}} element\u00f3w",
				},
			}),
		)
		return inst
	}

	t.Run("selects correct plural form for English", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "No items", inst.Tn("en", "general", "items", 0))
		require.Equal(t, "1 item", inst.Tn("en", "general", "items", 1))
		require.Equal(t, "2 items", inst.Tn("en", "general", "items", 2))
		require.Equal(t, "5 items", inst.Tn("en", "general", "items", 5))
		require.Equal(t, "100 items", inst.Tn("en", "general", "items", 100))
	})

	t.Run("selects correct plural form for Polish", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "Brak element\u00f3w", inst.Tn("pl", "general", "items", 0))
		require.Equal(t, "1 element", inst.Tn("pl", "general", "items", 1))
		require.Equal(t, "2 elementy", inst.Tn("pl", "general", "items", 2))
		require.Equal(t, "3 elementy", inst.Tn("pl", "general", "items", 3))
		require.Equal(t, "4 elementy", inst.Tn("pl", "general", "items", 4))
		require.Equal(t, "5 element\u00f3w", inst.Tn("pl", "general", "items", 5))
		require.Equal(t, "12 element\u00f3w", inst.Tn("pl", "general", "items", 12))
		require.Equal(t, "22 elementy", inst.Tn("pl", "general", "items", 22))
		require.Equal(t, "100 element\u00f3w", inst.Tn("pl", "general", "items", 100))
	})

	t.Run("falls back to other form when specific form not found", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "You have 0 new messages", inst.Tn("en", "general", "messages", 0))
	})

	t.Run("injects count placeholder automatically", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "5 items", inst.Tn("en", "general", "items", 5))
	})

	t.Run("merges additional placeholders with count", func(t *testing.T) {
		t.Parallel()
		inst, _ := i18n.New(
			i18n.WithTranslations("en", "general", map[string]any{
				"files": map[string]any{
					"one":   "{{count}} file in {{folder}}",
					"other": "{{count}} files in {{folder}}",
				},
			}),
		)
		result := inst.Tn("en", "general", "files", 3, i18n.M{"folder": "Documents"})
		require.Equal(t, "3 files in Documents", result)
	})

	t.Run("additional placeholders can override count", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.Tn("en", "general", "items", 5, i18n.M{"count": "many"})
		require.Equal(t, "many items", result)
	})

	t.Run("falls back to default language", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		result := inst.Tn("pl", "general", "messages", 3)
		require.Equal(t, "You have 3 new messages", result)
	})

	t.Run("returns key when translation not found", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "nonexistent", inst.Tn("en", "general", "nonexistent", 5))
	})

	t.Run("calls missing key handler for plural translations", func(t *testing.T) {
		t.Parallel()
		var missingKeys []string
		handler := func(lang, namespace, key string) {
			missingKeys = append(missingKeys, fmt.Sprintf("%s:%s:%s", lang, namespace, key))
		}

		inst, err := i18n.New(
			i18n.WithMissingKeyHandler(handler),
			i18n.WithTranslations("en", "test", map[string]any{
				"items": map[string]any{
					"one":   "1 item",
					"other": "{{count}} items",
				},
			}),
		)
		require.NoError(t, err)

		result := inst.Tn("en", "test", "items", 5)
		require.Equal(t, "5 items", result)
		require.Empty(t, missingKeys)

		result = inst.Tn("en", "test", "missing_plural", 5)
		require.Equal(t, "missing_plural", result)
		require.Equal(t, []string{"en:test:missing_plural"}, missingKeys)
	})

	t.Run("uses auto-assigned plural rule based on language code", func(t *testing.T) {
		t.Parallel()
		inst, _ := i18n.New(
			i18n.WithTranslations("fr", "general", map[string]any{
				"items": map[string]any{
					"one":   "{{count}} \u00e9l\u00e9ment",
					"many":  "{{count}} \u00e9l\u00e9ments (beaucoup)",
					"other": "{{count}} \u00e9l\u00e9ments",
				},
			}),
		)

		require.Equal(t, "0 \u00e9l\u00e9ment", inst.Tn("fr", "general", "items", 0))
		require.Equal(t, "1 \u00e9l\u00e9ment", inst.Tn("fr", "general", "items", 1))
		require.Equal(t, "3 \u00e9l\u00e9ments", inst.Tn("fr", "general", "items", 3))
		require.Equal(t, "10 \u00e9l\u00e9ments", inst.Tn("fr", "general", "items", 10))
		require.Equal(t, "100 \u00e9l\u00e9ments", inst.Tn("fr", "general", "items", 100))
		require.Equal(t, "1000000 \u00e9l\u00e9ments (beaucoup)", inst.Tn("fr", "general", "items", 1000000))
	})

	t.Run("handles negative numbers", func(t *testing.T) {
		t.Parallel()
		inst := setup()
		require.Equal(t, "-1 item", inst.Tn("en", "general", "items", -1))
		require.Equal(t, "-5 items", inst.Tn("en", "general", "items", -5))
	})
}

func TestFlattenTranslations(t *testing.T) {
	t.Parallel()

	t.Run("flattens nested structures correctly", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithTranslations("en", "test", map[string]any{
				"simple": "Simple value",
				"nested": map[string]any{
					"level1": "Level 1",
					"deeper": map[string]any{
						"level2": "Level 2",
						"evenDeeper": map[string]any{
							"level3": "Level 3",
						},
					},
				},
				"plural": map[string]string{
					"one":   "One item",
					"other": "Many items",
				},
				"number":  42,
				"boolean": true,
			}),
		)
		require.NoError(t, err)

		require.Equal(t, "Simple value", inst.T("en", "test", "simple"))
		require.Equal(t, "Level 1", inst.T("en", "test", "nested.level1"))
		require.Equal(t, "Level 2", inst.T("en", "test", "nested.deeper.level2"))
		require.Equal(t, "Level 3", inst.T("en", "test", "nested.deeper.evenDeeper.level3"))
		require.Equal(t, "One item", inst.T("en", "test", "plural.one"))
		require.Equal(t, "Many items", inst.T("en", "test", "plural.other"))
		require.Equal(t, "42", inst.T("en", "test", "number"))
		require.Equal(t, "true", inst.T("en", "test", "boolean"))
	})
}

func TestConcurrency(t *testing.T) {
	t.Parallel()

	t.Run("concurrent reads are safe", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithTranslations("en", "general", map[string]any{
				"hello": "Hello",
				"world": "World",
				"items": map[string]any{
					"one":   "{{count}} item",
					"other": "{{count}} items",
				},
			}),
		)
		require.NoError(t, err)

		done := make(chan bool, 100)
		for i := range 100 {
			go func(n int) {
				defer func() { done <- true }()

				switch n % 3 {
				case 0:
					result := inst.T("en", "general", "hello")
					assert.Equal(t, "Hello", result)
				case 1:
					result := inst.T("en", "general", "world")
					assert.Equal(t, "World", result)
				case 2:
					result := inst.Tn("en", "general", "items", n)
					if n == 1 {
						assert.Equal(t, "1 item", result)
					} else {
						assert.Contains(t, result, "items")
					}
				}
			}(i)
		}

		for range 100 {
			<-done
		}
	})
}
