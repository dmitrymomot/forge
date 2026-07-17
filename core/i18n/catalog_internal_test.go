package i18n

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFS(t *testing.T) {
	t.Parallel()
	cats, err := loadFS(os.DirFS("testdata/locales"))
	require.NoError(t, err)

	// The locale set is exactly the directories present — including vi, a
	// language this package knows nothing about.
	tags := make([]string, 0, len(cats))
	for tag := range cats {
		tags = append(tags, tag)
	}
	assert.ElementsMatch(t, []string{"en", "en-GB", "uk", "de", "vi"}, tags)

	en := cats["en"]
	require.NotNil(t, en)
	// Namespace becomes the leading key segment; nesting flattens to dots.
	assert.Equal(t, "Dashboard", en.messages["app.title"])
	assert.Equal(t, "Hello, {{name}}!", en.messages["app.greeting"])
	assert.Equal(t, "Save", en.messages["app.buttons.save"])
	assert.Equal(t, "Cancel", en.messages["app.buttons.cancel"])

	// A map of plural forms becomes a plural entry, not a namespace.
	assert.NotContains(t, en.messages, "cart.items")
	forms := en.plurals["cart.items"]
	require.NotNil(t, forms)
	assert.Equal(t, "Your cart is empty", forms[Zero])
	assert.Equal(t, "1 item in your cart", forms[One])
	assert.Equal(t, "{{count}} items in your cart", forms[Other])

	// uk/cart.json has no "other" form; loading must not invent one.
	uk := cats["uk"]
	require.NotNil(t, uk)
	ukForms := uk.plurals["cart.items"]
	require.NotNil(t, ukForms)
	assert.NotContains(t, ukForms, Other)
	assert.Equal(t, "{{count}} товарів у кошику", ukForms[Many])

	// Directory name is normalized into the tag.
	gb := cats["en-GB"]
	require.NotNil(t, gb)
	assert.Equal(t, "Dashboard (GB)", gb.messages["app.title"])
}

func TestIsPluralMap(t *testing.T) {
	t.Parallel()
	assert.True(t, isPluralMap(map[string]any{"one": "x", "other": "y"}))
	assert.True(t, isPluralMap(map[string]any{"other": "y"}))
	// A namespace, not a plural.
	assert.False(t, isPluralMap(map[string]any{"save": "Save"}))
	// Mixed keys: not all are categories.
	assert.False(t, isPluralMap(map[string]any{"one": "x", "save": "y"}))
	// A category key whose value is an object is a namespace.
	assert.False(t, isPluralMap(map[string]any{"other": map[string]any{"a": "b"}}))
	assert.False(t, isPluralMap(map[string]any{}))
}

func TestLoadFSErrors(t *testing.T) {
	t.Parallel()
	t.Run("bad json", func(t *testing.T) {
		t.Parallel()
		_, err := loadFS(fstest.MapFS{"en/app.json": &fstest.MapFile{Data: []byte("{oops")}})
		require.ErrorIs(t, err, ErrInvalidCatalog)
	})
	t.Run("unnormalizable dir", func(t *testing.T) {
		t.Parallel()
		_, err := loadFS(fstest.MapFS{"--/app.json": &fstest.MapFile{Data: []byte("{}")}})
		require.ErrorIs(t, err, ErrInvalidCatalog)
	})
	t.Run("duplicate key across files", func(t *testing.T) {
		t.Parallel()
		_, err := loadFS(fstest.MapFS{
			"en/app.json":  &fstest.MapFile{Data: []byte(`{"x": "1"}`)},
			"en/app2.json": &fstest.MapFile{Data: []byte(`{"x": "2"}`)},
		})
		// Different namespaces, so no collision: app.x vs app2.x.
		require.NoError(t, err)
	})
	t.Run("duplicate key same namespace", func(t *testing.T) {
		t.Parallel()
		// A plural entry and a plain message cannot share a key.
		_, err := loadFS(fstest.MapFS{
			"en/app.json": &fstest.MapFile{Data: []byte(`{"x": {"one": "a", "other": "b"}, "x.one": "c"}`)},
		})
		require.ErrorIs(t, err, ErrDuplicateKey)
	})
}

func TestLoadFSScalars(t *testing.T) {
	t.Parallel()
	cats, err := loadFS(fstest.MapFS{
		"en/app.json": &fstest.MapFile{Data: []byte(`{"n": 42, "b": true}`)},
	})
	require.NoError(t, err)
	en := cats["en"]
	require.NotNil(t, en)
	assert.Equal(t, "42", en.messages["app.n"])
	assert.Equal(t, "true", en.messages["app.b"])
}

func TestLoadFSIgnoresNonJSON(t *testing.T) {
	t.Parallel()
	cats, err := loadFS(fstest.MapFS{
		"en/app.json":  &fstest.MapFile{Data: []byte(`{"a": "b"}`)},
		"en/README.md": &fstest.MapFile{Data: []byte(`not json`)},
		"README.md":    &fstest.MapFile{Data: []byte(`not json`)},
	})
	require.NoError(t, err)
	require.Len(t, cats, 1)
	en := cats["en"]
	require.NotNil(t, en)
	assert.Equal(t, "b", en.messages["app.a"])
}
