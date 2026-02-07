package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
	"github.com/dmitrymomot/forge/pkg/i18n"
)

func newI18nService(t *testing.T) *i18n.I18n {
	t.Helper()
	svc, err := i18n.New(
		i18n.WithDefaultLanguage("en"),
		i18n.WithLanguages("en", "de", "pl"),
		i18n.WithTranslations("en", "common", map[string]any{
			"hello": "Hello",
			"items": map[string]any{
				"one":   "{{count}} item",
				"other": "{{count}} items",
			},
		}),
		i18n.WithTranslations("de", "common", map[string]any{
			"hello": "Hallo",
			"items": map[string]any{
				"one":   "{{count}} Artikel",
				"other": "{{count}} Artikel",
			},
		}),
		i18n.WithTranslations("pl", "common", map[string]any{
			"hello": "Cześć",
		}),
	)
	require.NoError(t, err)
	return svc
}

func TestI18nMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("stores translator in context", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		mw := middlewares.I18n(svc, middlewares.WithI18nNamespace("common"))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", "en")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotTranslator *i18n.Translator
		handler := mw(func(c internal.Context) error {
			gotTranslator = middlewares.GetTranslator(c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.NotNil(t, gotTranslator)
		require.Equal(t, "Hello", gotTranslator.T("hello"))
	})

	t.Run("stores language in context", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		mw := middlewares.I18n(svc, middlewares.WithI18nNamespace("common"))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", "de")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotLang string
		handler := mw(func(c internal.Context) error {
			gotLang = middlewares.GetLanguage(c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Equal(t, "de", gotLang)
	})

	t.Run("default extractor uses cookie then accept-language", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		mw := middlewares.I18n(svc, middlewares.WithI18nNamespace("common"))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: "lang", Value: "pl"})
		r.Header.Set("Accept-Language", "de")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotLang string
		handler := mw(func(c internal.Context) error {
			gotLang = middlewares.GetLanguage(c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Equal(t, "pl", gotLang)
	})

	t.Run("default extractor falls back to accept-language when no cookie", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		mw := middlewares.I18n(svc, middlewares.WithI18nNamespace("common"))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", "de-DE,de;q=0.9")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotLang string
		handler := mw(func(c internal.Context) error {
			gotLang = middlewares.GetLanguage(c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Equal(t, "de", gotLang)
	})

	t.Run("custom extractor chain", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		ext := internal.NewExtractor(
			internal.FromQuery("locale"),
		)
		mw := middlewares.I18n(svc,
			middlewares.WithI18nNamespace("common"),
			middlewares.WithI18nExtractor(ext),
		)

		r := httptest.NewRequest(http.MethodGet, "/?locale=de", nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotLang string
		handler := mw(func(c internal.Context) error {
			gotLang = middlewares.GetLanguage(c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Equal(t, "de", gotLang)
	})

	t.Run("format map selects correct format per language", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		deFormat := i18n.FormatDeDE()
		mw := middlewares.I18n(svc,
			middlewares.WithI18nNamespace("common"),
			middlewares.WithI18nFormatMap(map[string]*i18n.LocaleFormat{
				"de": deFormat,
			}),
		)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", "de")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotFormat *i18n.LocaleFormat
		handler := mw(func(c internal.Context) error {
			tr := middlewares.GetTranslator(c)
			gotFormat = tr.Format()
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Equal(t, deFormat, gotFormat)
	})

	t.Run("fallback to default format when language not in format map", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		customDefault := i18n.FormatEnGB()
		mw := middlewares.I18n(svc,
			middlewares.WithI18nNamespace("common"),
			middlewares.WithI18nDefaultFormat(customDefault),
			middlewares.WithI18nFormatMap(map[string]*i18n.LocaleFormat{
				"de": i18n.FormatDeDE(),
			}),
		)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", "pl")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotFormat *i18n.LocaleFormat
		handler := mw(func(c internal.Context) error {
			tr := middlewares.GetTranslator(c)
			gotFormat = tr.Format()
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Equal(t, customDefault, gotFormat)
	})

	t.Run("fallback to default language when no extractor matches", func(t *testing.T) {
		t.Parallel()
		svc := newI18nService(t)
		mw := middlewares.I18n(svc, middlewares.WithI18nNamespace("common"))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotLang string
		handler := mw(func(c internal.Context) error {
			gotLang = middlewares.GetLanguage(c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Equal(t, "en", gotLang)
	})
}

func TestGetTranslatorWithoutMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when middleware not used", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		tr := middlewares.GetTranslator(c)
		require.Nil(t, tr)
	})
}

func TestGetLanguageWithoutMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("returns empty when middleware not used", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		lang := middlewares.GetLanguage(c)
		require.Empty(t, lang)
	})
}

func TestFromAcceptLanguage(t *testing.T) {
	t.Parallel()

	t.Run("parses accept-language header", func(t *testing.T) {
		t.Parallel()
		available := []string{"en", "de", "pl"}
		source := middlewares.FromAcceptLanguage(available)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", "de-DE,de;q=0.9,en;q=0.8")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		val, ok := source(c)
		require.True(t, ok)
		require.Equal(t, "de", val)
	})

	t.Run("returns false when header is empty", func(t *testing.T) {
		t.Parallel()
		available := []string{"en", "de"}
		source := middlewares.FromAcceptLanguage(available)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		_, ok := source(c)
		require.False(t, ok)
	})

	t.Run("returns first available when no match", func(t *testing.T) {
		t.Parallel()
		available := []string{"en", "de"}
		source := middlewares.FromAcceptLanguage(available)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Language", "ja")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		val, ok := source(c)
		require.True(t, ok)
		require.Equal(t, "en", val)
	})
}
