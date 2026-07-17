package i18n_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
	"github.com/dmitrymomot/forge/core/money"
)

func TestWithLocaleAndOneLiners(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	ctx := b.WithLocale(context.Background(), b.ParseOrDefault("uk"))

	assert.Equal(t, "uk", i18n.LocaleFrom(ctx).Tag())
	assert.Equal(t, "Панель", i18n.T(ctx, "app.title"))
	assert.Equal(t, "Привіт, Олю!", i18n.T(ctx, "app.greeting", "name", "Олю"))
	assert.Equal(t, "Dashboard", i18n.T(b.WithLocale(context.Background(), b.Default()), "app.title"))
	assert.Equal(t, "Панель", i18n.TK(ctx, keyTitle))
}

func TestOneLinersFormatting(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	ctx := b.WithLocale(context.Background(), b.ParseOrDefault("de"))
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	m := money.FromMinor(123456, money.Currency{Code: "EUR", Num: "978", Symbol: "€", MinorUnits: 2})

	assert.Equal(t, "1.234,5", i18n.Number(ctx, 1234.5))
	assert.Equal(t, "1.234", i18n.NumberInt(ctx, 1234))
	assert.Equal(t, "50 %", i18n.Percent(ctx, 0.5))
	assert.Equal(t, "1.234,56 €", i18n.Currency(ctx, m))
	assert.Equal(t, "17.07.2026", i18n.Date(ctx, ts))
	assert.Equal(t, "15:04", i18n.Time(ctx, ts))
	assert.Equal(t, "17.07.2026 15:04", i18n.DateTime(ctx, ts))
}

func TestOneLinersFailClosedWithoutLocalizer(t *testing.T) {
	t.Parallel()
	// A background job, or any request that never passed through Middleware.
	ctx := context.Background()
	assert.True(t, i18n.LocaleFrom(ctx).IsZero())
	assert.True(t, i18n.FromContext(ctx).IsZero())

	assert.Equal(t, "app.title", i18n.T(ctx, "app.title"))
	assert.Equal(t, "cart.items", i18n.TN(ctx, "cart.items", 3))
	assert.Equal(t, "app.title", i18n.TK(ctx, keyTitle))
	assert.Equal(t, "cart.items", i18n.TNK(ctx, keyItems, 3))
	assert.Equal(t, "1,234.5", i18n.Number(ctx, 1234.5))
	assert.Equal(t, "50%", i18n.Percent(ctx, 0.5))
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	assert.Equal(t, "2026-07-17", i18n.Date(ctx, ts))
}

func TestBundleCtxFallsBackToBundleDefault(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	// Bundle.Ctx has a bundle to fall back to, so it uses that bundle's
	// default rather than the zero Localizer.
	l := b.Ctx(context.Background())
	assert.False(t, l.IsZero())
	assert.Equal(t, "en", l.Locale().Tag())
	assert.Equal(t, "Dashboard", l.T("app.title"))

	// With a localizer present, it returns that one.
	ctx := b.WithLocale(context.Background(), b.ParseOrDefault("uk"))
	assert.Equal(t, "uk", b.Ctx(ctx).Locale().Tag())
}

func TestWithLocaleUnknownTagUsesDefault(t *testing.T) {
	t.Parallel()
	b := fmtBundle(t)
	// A job with an unknown recipient locale is a first-class path.
	ctx := b.WithLocale(context.Background(), b.ParseOrDefault(""))
	assert.Equal(t, "en", i18n.LocaleFrom(ctx).Tag())
	assert.Equal(t, "Dashboard", i18n.T(ctx, "app.title"))
}

func TestTwoBundlesInOneProcess(t *testing.T) {
	t.Parallel()
	// The ctx carries a Localizer, which carries its own bundle — so two
	// bundles coexist without a global.
	b1 := fmtBundle(t)
	b2, err := i18n.New(i18n.WithTranslations("en", "app", map[string]any{"title": "Other"}))
	require.NoError(t, err)

	ctx1 := b1.WithLocale(context.Background(), b1.Default())
	ctx2 := b2.WithLocale(context.Background(), b2.Default())
	assert.Equal(t, "Dashboard", i18n.T(ctx1, "app.title"))
	assert.Equal(t, "Other", i18n.T(ctx2, "app.title"))
}
