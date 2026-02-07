package i18n_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

func TestNewTranslator(t *testing.T) {
	t.Parallel()

	inst, err := i18n.New(
		i18n.WithDefaultLanguage("en"),
		i18n.WithTranslations("en", "test", map[string]any{
			"hello":   "Hello",
			"welcome": "Welcome, {{name}}!",
			"items": map[string]any{
				"one":   "{{count}} item",
				"other": "{{count}} items",
			},
		}),
	)
	require.NoError(t, err)

	t.Run("panics with nil i18n", func(t *testing.T) {
		t.Parallel()
		require.Panics(t, func() {
			i18n.NewTranslator(nil, "en", "test", nil)
		})
	})

	t.Run("defaults to i18n default language when empty", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "", "test", nil)
		require.Equal(t, "en", tr.Language())
	})

	t.Run("defaults to FormatEnUS when format is nil", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		require.NotNil(t, tr.Format())
		require.Equal(t, "$1,234.50", tr.FormatCurrency(1234.50))
	})

	t.Run("uses provided format", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", i18n.FormatDeDE())
		require.Equal(t, "1.234,50 \u20ac", tr.FormatCurrency(1234.50))
	})

	t.Run("translates keys", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		require.Equal(t, "Hello", tr.T("hello"))
	})

	t.Run("translates keys with placeholders", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		require.Equal(t, "Welcome, Alice!", tr.T("welcome", i18n.M{"name": "Alice"}))
	})

	t.Run("translates plural keys", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		require.Equal(t, "1 item", tr.Tn("items", 1))
		require.Equal(t, "5 items", tr.Tn("items", 5))
	})

	t.Run("returns namespace", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		require.Equal(t, "test", tr.Namespace())
	})

	t.Run("TranslateMessage without placeholders", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		require.Equal(t, tr.T("hello"), tr.TranslateMessage("hello", nil))
	})

	t.Run("TranslateMessage with placeholders", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		values := map[string]any{"name": "Alice"}
		require.Equal(t, tr.T("welcome", values), tr.TranslateMessage("welcome", values))
	})
}

func TestTranslatorFormatting(t *testing.T) {
	t.Parallel()

	inst, err := i18n.New(
		i18n.WithTranslations("en", "test", map[string]any{"x": "x"}),
	)
	require.NoError(t, err)

	t.Run("default English format", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)

		require.Equal(t, "1,234.5", tr.FormatNumber(1234.5))
		require.Equal(t, "$1,234.50", tr.FormatCurrency(1234.50))
		require.Equal(t, "25.5%", tr.FormatPercent(0.255))

		testDate := time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)
		require.Equal(t, "01/02/2024", tr.FormatDate(testDate))
		require.Equal(t, "3:04 PM", tr.FormatTime(testDate))
		require.Equal(t, "01/02/2024 3:04 PM", tr.FormatDateTime(testDate))
	})

	t.Run("custom format", func(t *testing.T) {
		t.Parallel()
		customFormat := i18n.NewLocaleFormat(
			i18n.WithDecimalSeparator(","),
			i18n.WithThousandSeparator("."),
			i18n.WithCurrencySymbol("\u20ac"),
			i18n.WithCurrencyPosition("after"),
			i18n.WithDateFormat("02.01.2006"),
			i18n.WithTimeFormat("15:04"),
			i18n.WithDateTimeFormat("02.01.2006 15:04"),
		)

		tr := i18n.NewTranslator(inst, "en", "test", customFormat)

		require.Equal(t, "1.234,5", tr.FormatNumber(1234.5))
		require.Equal(t, "1.234,50 \u20ac", tr.FormatCurrency(1234.50))
		require.Equal(t, "25,5%", tr.FormatPercent(0.255))

		testDate := time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)
		require.Equal(t, "02.01.2024", tr.FormatDate(testDate))
		require.Equal(t, "15:04", tr.FormatTime(testDate))
		require.Equal(t, "02.01.2024 15:04", tr.FormatDateTime(testDate))
	})

	t.Run("access format from translator", func(t *testing.T) {
		t.Parallel()
		tr := i18n.NewTranslator(inst, "en", "test", nil)
		format := tr.Format()
		require.NotNil(t, format)
		require.Equal(t, "1,234.5", format.FormatNumber(1234.5))
	})
}
