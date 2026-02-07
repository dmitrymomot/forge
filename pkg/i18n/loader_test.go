package i18n_test

import (
	"embed"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/i18n"
)

//go:embed testdata
var testdataFS embed.FS

func TestWithJSONDir(t *testing.T) {
	t.Parallel()

	subFS, err := fs.Sub(testdataFS, "testdata")
	require.NoError(t, err)

	t.Run("loads JSON translations from fs.FS", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithJSONDir(subFS),
		)
		require.NoError(t, err)

		require.Equal(t, "Hello", inst.T("en", "common", "hello"))
		require.Equal(t, "Welcome, Alice!", inst.T("en", "common", "welcome", i18n.M{"name": "Alice"}))
		require.Equal(t, "Save", inst.T("en", "common", "buttons.save"))
		require.Equal(t, "Cancel", inst.T("en", "common", "buttons.cancel"))
	})

	t.Run("loads multiple namespaces", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithJSONDir(subFS),
		)
		require.NoError(t, err)

		require.Equal(t, "Resource not found", inst.T("en", "errors", "not_found"))
		require.Equal(t, "Field email is required", inst.T("en", "errors", "validation.required", i18n.M{"field": "email"}))
	})

	t.Run("loads multiple languages", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithJSONDir(subFS),
		)
		require.NoError(t, err)

		require.Equal(t, "Hallo", inst.T("de", "common", "hello"))
		require.Equal(t, "Willkommen, Hans!", inst.T("de", "common", "welcome", i18n.M{"name": "Hans"}))
		require.Equal(t, "Speichern", inst.T("de", "common", "buttons.save"))
	})

	t.Run("combines with WithTranslations", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithJSONDir(subFS),
			i18n.WithTranslations("en", "extra", map[string]any{
				"test": "Test value",
			}),
		)
		require.NoError(t, err)

		require.Equal(t, "Hello", inst.T("en", "common", "hello"))
		require.Equal(t, "Test value", inst.T("en", "extra", "test"))
	})
}

func TestWithYAMLDir(t *testing.T) {
	t.Parallel()

	subFS, err := fs.Sub(testdataFS, "testdata")
	require.NoError(t, err)

	t.Run("loads YAML translations from fs.FS", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("fr"),
			i18n.WithYAMLDir(subFS),
		)
		require.NoError(t, err)

		require.Equal(t, "Bonjour", inst.T("fr", "common", "hello"))
		require.Equal(t, "Bienvenue, Marie!", inst.T("fr", "common", "welcome", i18n.M{"name": "Marie"}))
		require.Equal(t, "Enregistrer", inst.T("fr", "common", "buttons.save"))
		require.Equal(t, "Annuler", inst.T("fr", "common", "buttons.cancel"))
	})
}

func TestWithJSONDirAndYAMLDir(t *testing.T) {
	t.Parallel()

	subFS, err := fs.Sub(testdataFS, "testdata")
	require.NoError(t, err)

	t.Run("loads both JSON and YAML", func(t *testing.T) {
		t.Parallel()
		inst, err := i18n.New(
			i18n.WithDefaultLanguage("en"),
			i18n.WithJSONDir(subFS),
			i18n.WithYAMLDir(subFS),
		)
		require.NoError(t, err)

		// JSON loaded
		require.Equal(t, "Hello", inst.T("en", "common", "hello"))
		require.Equal(t, "Hallo", inst.T("de", "common", "hello"))

		// YAML loaded
		require.Equal(t, "Bonjour", inst.T("fr", "common", "hello"))
	})
}
