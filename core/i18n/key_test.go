package i18n_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
)

var (
	keyTitle = i18n.NewKey("app.title")
	keyItems = i18n.NewKey("cart.items")
	keyTypo  = i18n.NewKey("app.ttile")
)

func TestKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "app.title", keyTitle.String())
	assert.Equal(t, keyTitle, i18n.NewKey("app.title"), "Key must be comparable by value")
}

func TestTK(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	en := b.Default()
	uk := b.ParseOrDefault("uk")

	assert.Equal(t, "Dashboard", b.TK(en, keyTitle))
	assert.Equal(t, "Панель", b.TK(uk, keyTitle))
	// TK matches T exactly — same lookup, same fallback.
	assert.Equal(t, b.T(uk, "app.title"), b.TK(uk, keyTitle))
	// An unknown key echoes, like T.
	assert.Equal(t, "app.ttile", b.TK(en, keyTypo))
}

func TestTNK(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	en := b.Default()
	assert.Equal(t, "Your cart is empty", b.TNK(en, keyItems, 0))
	assert.Equal(t, "1 item in your cart", b.TNK(en, keyItems, 1))
	assert.Equal(t, "7 items in your cart", b.TNK(en, keyItems, 7))
	assert.Equal(t, b.TN(en, "cart.items", 7), b.TNK(en, keyItems, 7))
}

func TestValidateKeys(t *testing.T) {
	t.Parallel()
	b := newBundle(t)

	require.NoError(t, b.ValidateKeys(keyTitle, keyItems))
	// Plural keys validate too: cart.items is a plural entry, not a message.
	require.NoError(t, b.ValidateKeys(keyItems))
	// No keys is not an error.
	require.NoError(t, b.ValidateKeys())

	err := b.ValidateKeys(keyTitle, keyTypo)
	require.ErrorIs(t, err, i18n.ErrUnknownKey)
	assert.Contains(t, err.Error(), "app.ttile", "the error must name the offending key")
}

func TestValidateKeysChecksDefaultLocaleOnly(t *testing.T) {
	t.Parallel()
	// app.only_en exists only in en (the default), so it validates even though
	// uk lacks it — every lookup falls through to the default anyway.
	b := newBundle(t)
	require.NoError(t, b.ValidateKeys(i18n.NewKey("app.only_en")))
}

func TestValidateKeysReportsAllMissing(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	err := b.ValidateKeys(i18n.NewKey("a.b"), keyTitle, i18n.NewKey("c.d"))
	require.ErrorIs(t, err, i18n.ErrUnknownKey)
	assert.Contains(t, err.Error(), "a.b")
	assert.Contains(t, err.Error(), "c.d")
	assert.NotContains(t, err.Error(), "app.title")
}

func TestValidateKeysUsesConfiguredDefault(t *testing.T) {
	t.Parallel()
	// With uk as the default, a key only en defines must fail validation.
	b, err := i18n.New(
		i18n.WithConfig(i18n.Config{DefaultLocale: "uk", CookieName: "lang", QueryParam: "lang"}),
		i18n.WithMessages(os.DirFS("testdata/locales")),
	)
	require.NoError(t, err)
	require.ErrorIs(t, b.ValidateKeys(i18n.NewKey("app.only_en")), i18n.ErrUnknownKey)
}
