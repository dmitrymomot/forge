package internal_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/i18n"
)

func newTestI18nService(t *testing.T) *i18n.I18n {
	t.Helper()
	svc, err := i18n.New(
		i18n.WithDefaultLanguage("en"),
		i18n.WithLanguages("en", "de"),
		i18n.WithTranslations("en", "common", map[string]any{
			"hello":   "Hello",
			"welcome": "Welcome, {{name}}!",
			"items": map[string]any{
				"one":   "{{count}} item",
				"other": "{{count}} items",
			},
			"validation.required": "{{field}} is required",
		}),
		i18n.WithTranslations("de", "common", map[string]any{
			"hello":   "Hallo",
			"welcome": "Willkommen, {{name}}!",
			"items": map[string]any{
				"one":   "{{count}} Artikel",
				"other": "{{count}} Artikel",
			},
			"validation.required": "{{field}} ist erforderlich",
		}),
	)
	require.NoError(t, err)
	return svc
}

func TestContextT(t *testing.T) {
	t.Parallel()

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", nil)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)

			require.Equal(t, "Hello", c.T("hello"))
			require.Equal(t, "Welcome, Alice!", c.T("welcome", i18n.M{"name": "Alice"}))
		})
	})

	t.Run("without translator returns key", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "hello", c.T("hello"))
			require.Equal(t, "welcome", c.T("welcome", i18n.M{"name": "Alice"}))
		})
	})
}

func TestContextTn(t *testing.T) {
	t.Parallel()

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", nil)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)

			require.Equal(t, "1 item", c.Tn("items", 1, i18n.M{"count": 1}))
			require.Equal(t, "5 items", c.Tn("items", 5, i18n.M{"count": 5}))
		})
	})

	t.Run("without translator returns key", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "items", c.Tn("items", 5, i18n.M{"count": 5}))
		})
	})
}

func TestContextLanguage(t *testing.T) {
	t.Parallel()

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "de", "common", nil)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)
			require.Equal(t, "de", c.Language())
		})
	})

	t.Run("without translator returns empty", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "", c.Language())
		})
	})
}

func TestContextFormatNumber(t *testing.T) {
	t.Parallel()

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", i18n.FormatEnUS())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)
			result := c.FormatNumber(1234567.89)
			require.NotEmpty(t, result)
			require.Contains(t, result, "1")
		})
	})

	t.Run("without translator uses fallback", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "1.23456789e+06", c.FormatNumber(1234567.89))
		})
	})
}

func TestContextFormatCurrency(t *testing.T) {
	t.Parallel()

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", i18n.FormatEnUS())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)
			result := c.FormatCurrency(99.99)
			require.NotEmpty(t, result)
		})
	})

	t.Run("without translator uses fallback", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "99.99", c.FormatCurrency(99.99))
		})
	})
}

func TestContextFormatPercent(t *testing.T) {
	t.Parallel()

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", i18n.FormatEnUS())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)
			result := c.FormatPercent(0.5)
			require.NotEmpty(t, result)
		})
	})

	t.Run("without translator uses fallback", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "50%", c.FormatPercent(0.5))
		})
	})
}

func TestContextFormatDate(t *testing.T) {
	t.Parallel()

	date := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", i18n.FormatEnUS())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)
			result := c.FormatDate(date)
			require.NotEmpty(t, result)
		})
	})

	t.Run("without translator uses fallback", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "2025-06-15", c.FormatDate(date))
		})
	})
}

func TestContextFormatTime(t *testing.T) {
	t.Parallel()

	tm := time.Date(2025, 1, 1, 14, 30, 45, 0, time.UTC)

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", i18n.FormatEnUS())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)
			result := c.FormatTime(tm)
			require.NotEmpty(t, result)
		})
	})

	t.Run("without translator uses fallback", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "14:30:45", c.FormatTime(tm))
		})
	})
}

func TestContextFormatDateTime(t *testing.T) {
	t.Parallel()

	dt := time.Date(2025, 6, 15, 14, 30, 45, 0, time.UTC)

	t.Run("with translator", func(t *testing.T) {
		t.Parallel()

		svc := newTestI18nService(t)
		tr := i18n.NewTranslator(svc, "en", "common", i18n.FormatEnUS())

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)
			result := c.FormatDateTime(dt)
			require.NotEmpty(t, result)
		})
	})

	t.Run("without translator uses fallback", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		requestVia(t, req, nil, func(c internal.Context) {
			require.Equal(t, "2025-06-15 14:30:45", c.FormatDateTime(dt))
		})
	})
}

func TestBindAutoTranslatesWithTranslator(t *testing.T) {
	t.Parallel()

	t.Run("auto-translates validation errors when translator is set", func(t *testing.T) {
		t.Parallel()

		svc, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithLanguages("en"),
			i18n.WithTranslations("en", "common", map[string]any{
				"validation.required": "{{field}} is required",
			}),
		)
		require.NoError(t, err)
		tr := i18n.NewTranslator(svc, "en", "common", nil)

		// POST with empty body to trigger required validation
		form := url.Values{}
		form.Set("name", "")
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		requestVia(t, req, nil, func(c internal.Context) {
			c.Set(internal.TranslatorKey{}, tr)

			type input struct {
				Name string `form:"name" validate:"required"`
			}
			var in input
			verrs, sysErr := c.Bind(&in)
			require.NoError(t, sysErr)
			require.NotNil(t, verrs)
			require.True(t, verrs.Has("Name"))

			// The message should be translated (not the raw translation key)
			for _, ve := range verrs {
				if ve.Field == "Name" {
					require.NotEqual(t, ve.TranslationKey, ve.Message,
						"message should be translated, not the raw key")
				}
			}
		})
	})

	t.Run("returns untranslated errors when no translator", func(t *testing.T) {
		t.Parallel()

		form := url.Values{}
		form.Set("name", "")
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		requestVia(t, req, nil, func(c internal.Context) {
			type input struct {
				Name string `form:"name" validate:"required"`
			}
			var in input
			verrs, sysErr := c.Bind(&in)
			require.NoError(t, sysErr)
			require.NotNil(t, verrs)
			require.True(t, verrs.Has("Name"))

			// Without translator, message should still contain the original message
			for _, ve := range verrs {
				if ve.Field == "Name" {
					require.NotEmpty(t, ve.Message)
				}
			}
		})
	})
}
