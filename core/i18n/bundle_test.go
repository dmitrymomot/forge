package i18n_test

import (
	"os"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
)

func newBundle(t *testing.T, opts ...i18n.Option) *i18n.Bundle {
	t.Helper()
	base := []i18n.Option{i18n.WithMessages(os.DirFS("testdata/locales"))}
	b, err := i18n.New(append(base, opts...)...)
	require.NoError(t, err)
	return b
}

func TestT(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	en := b.ParseOrDefault("en")
	uk := b.ParseOrDefault("uk")

	assert.Equal(t, "Dashboard", b.T(en, "app.title"))
	assert.Equal(t, "Привіт, Олю!", b.T(uk, "app.greeting", "name", "Олю"))
	assert.Equal(t, "Зберегти", b.T(uk, "app.buttons.save"))
	// uk lacks only_en, so it falls through to the default locale.
	assert.Equal(t, "English only", b.T(uk, "app.only_en"))
	// A key nobody defines echoes back.
	assert.Equal(t, "app.nope", b.T(en, "app.nope"))
}

func TestTSupportsUnknownLanguage(t *testing.T) {
	t.Parallel()
	// vi is a language this package ships no knowledge of. It must work
	// exactly like any other — this is the whole point of the design.
	b := newBundle(t)
	vi, err := b.Parse("vi")
	require.NoError(t, err)
	assert.Equal(t, "vi", vi.Tag())
	assert.Equal(t, "Bảng điều khiển", b.T(vi, "app.title"))
	// A key vi lacks falls through to the default.
	assert.Equal(t, "English only", b.T(vi, "app.only_en"))
}

func TestRegionalLayering(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	gb, err := b.Parse("en-GB")
	require.NoError(t, err)
	// en-GB defines title...
	assert.Equal(t, "Dashboard (GB)", b.T(gb, "app.title"))
	// ...but not greeting, which layers through to en.
	assert.Equal(t, "Hello, Ann!", b.T(gb, "app.greeting", "name", "Ann"))
}

func TestParse(t *testing.T) {
	t.Parallel()
	b := newBundle(t)

	// Exact match.
	loc, err := b.Parse("uk")
	require.NoError(t, err)
	assert.Equal(t, "uk", loc.Tag())

	// Normalization at the boundary.
	loc, err = b.Parse("EN_gb")
	require.NoError(t, err)
	assert.Equal(t, "en-GB", loc.Tag())

	// Region falls back to base language, and Parse reports what you actually got.
	loc, err = b.Parse("uk-UA")
	require.NoError(t, err)
	assert.Equal(t, "uk", loc.Tag())

	// Unsupported and malformed fail.
	_, err = b.Parse("ww-WW")
	require.ErrorIs(t, err, i18n.ErrUnknownLocale)
	_, err = b.Parse("")
	require.ErrorIs(t, err, i18n.ErrUnknownLocale)

	// ParseOrDefault never fails.
	assert.Equal(t, b.Default(), b.ParseOrDefault("ww-WW"))
	assert.Equal(t, b.Default(), b.ParseOrDefault(""))
	assert.Equal(t, "en", b.Default().Tag())
}

func TestLocales(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	tags := make([]string, 0, len(b.Locales()))
	for _, l := range b.Locales() {
		tags = append(tags, l.Tag())
	}
	assert.ElementsMatch(t, []string{"en", "en-GB", "uk", "de", "vi"}, tags)
	// Locales documents "sorted by tag", which is also what makes the locale
	// indices behind it deterministic across runs.
	assert.IsIncreasing(t, tags)
}

func TestZeroLocaleUsesDefault(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	var zero i18n.Locale
	assert.Equal(t, "Dashboard", b.T(zero, "app.title"))
}

func TestTNDefaultRule(t *testing.T) {
	t.Parallel()
	// No rule wired for en: the built-in zero-one-many default applies.
	b := newBundle(t)
	en := b.Default()
	// zero form defined in the catalog and DefaultRule(0) == Zero.
	assert.Equal(t, "Your cart is empty", b.TN(en, "cart.items", 0))
	assert.Equal(t, "1 item in your cart", b.TN(en, "cart.items", 1))
	// DefaultRule(5) == Many; en has no many form, so many→other resolves it.
	assert.Equal(t, "5 items in your cart", b.TN(en, "cart.items", 5))
}

func TestTNWiredRule(t *testing.T) {
	t.Parallel()
	// A real Ukrainian rule, wired explicitly. Declared inline: core ships no
	// language data, and this test must not depend on cldr.
	ukRule := func(n int) i18n.PluralCategory {
		if n < 0 {
			n = -n
		}
		m10, m100 := n%10, n%100
		switch {
		case m10 == 1 && m100 != 11:
			return i18n.One
		case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
			return i18n.Few
		default:
			return i18n.Many
		}
	}
	b := newBundle(t, i18n.WithPlural("uk", ukRule))
	uk := b.ParseOrDefault("uk")

	assert.Equal(t, "1 товар у кошику", b.TN(uk, "cart.items", 1))
	assert.Equal(t, "21 товар у кошику", b.TN(uk, "cart.items", 21)) // 21 → one, not many
	assert.Equal(t, "2 товари у кошику", b.TN(uk, "cart.items", 2))
	assert.Equal(t, "5 товарів у кошику", b.TN(uk, "cart.items", 5))
	assert.Equal(t, "11 товарів у кошику", b.TN(uk, "cart.items", 11))
	// uk's rule never produces Other, and uk/cart.json defines no other form.
	// Nothing above needed one — that is correct, not a gap.
}

func TestTNUsesRuleOfLocaleWhereMessageFound(t *testing.T) {
	t.Parallel()
	// de has no cart.json, so cart.items resolves through the default (en)
	// catalog. The form must be selected with en's rule — the rule of the
	// locale the message was FOUND in — not with de's.
	//
	// de's rule sends every count to Many, and en's catalog defines no many
	// form: selecting with de's rule would fall back many→other and render
	// "1 items in your cart". en's own DefaultRule sends 1 to One, which en
	// does define. The two answers differ, so this pins the decision down.
	alwaysMany := func(int) i18n.PluralCategory { return i18n.Many }
	b := newBundle(t, i18n.WithPlural("de", alwaysMany))
	de := b.ParseOrDefault("de")
	assert.Equal(t, "1 item in your cart", b.TN(de, "cart.items", 1))
}

func TestTNExplicitCountOverride(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	en := b.Default()
	assert.Equal(t, "many items in your cart", b.TN(en, "cart.items", 5, "count", "many"))
}

func TestTNFallsBackAcrossLocales(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	// de has no cart.json at all: the whole plural entry resolves via the
	// default locale's catalog.
	de := b.ParseOrDefault("de")
	assert.Equal(t, "3 items in your cart", b.TN(de, "cart.items", 3))
}

func TestMissHandler(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var misses []i18n.Miss
	b := newBundle(t, i18n.WithMissingHandler(func(m i18n.Miss) {
		mu.Lock()
		defer mu.Unlock()
		misses = append(misses, m)
	}))
	assert.Equal(t, "app.nope", b.T(b.Default(), "app.nope"))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, misses, 1)
	assert.Equal(t, "app.nope", misses[0].Key)
	assert.Equal(t, i18n.MissingKey, misses[0].Reason)
	assert.Equal(t, "en", misses[0].Locale.Tag())
}

func TestProbeOnlyRunsForWiredRules(t *testing.T) {
	t.Parallel()
	// With no rule wired, DefaultRule applies and probing must stay silent —
	// an ordinary {zero,one,other} catalog is not "incomplete" with respect to
	// a rule that is not a grammar claim.
	var mu sync.Mutex
	var forms []i18n.Miss
	newBundle(t, i18n.WithMissingHandler(func(m i18n.Miss) {
		if m.Reason == i18n.MissingForm {
			mu.Lock()
			forms = append(forms, m)
			mu.Unlock()
		}
	}))
	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, forms, "probing must not fire for default-rule locales")
}

func TestProbeReportsIncompleteTranslation(t *testing.T) {
	t.Parallel()
	// A rule producing one/few/many against a catalog defining only one/few:
	// "many" is a real gap and must be reported.
	rule := func(n int) i18n.PluralCategory {
		switch {
		case n == 1:
			return i18n.One
		case n >= 2 && n <= 4:
			return i18n.Few
		default:
			return i18n.Many
		}
	}
	var mu sync.Mutex
	var keys []string
	_, err := i18n.New(
		i18n.WithMessages(fstest.MapFS{
			"en/app.json": &fstest.MapFile{Data: []byte(`{"n": {"one": "a", "few": "b"}}`)},
		}),
		i18n.WithPlural("en", rule),
		i18n.WithMissingHandler(func(m i18n.Miss) {
			if m.Reason == i18n.MissingForm {
				mu.Lock()
				keys = append(keys, m.Key)
				mu.Unlock()
			}
		}),
	)
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"app.n.many"}, keys)
}

func TestProbeReportsDeadForm(t *testing.T) {
	t.Parallel()
	// A rule that never produces "two" against a catalog defining it: dead
	// translation. But "zero" (convention) and "other" (terminal) never count.
	rule := func(n int) i18n.PluralCategory {
		if n == 1 {
			return i18n.One
		}
		return i18n.Other
	}
	var mu sync.Mutex
	var keys []string
	_, err := i18n.New(
		i18n.WithMessages(fstest.MapFS{
			"en/app.json": &fstest.MapFile{Data: []byte(
				`{"n": {"one": "a", "two": "b", "other": "c", "zero": "d"}}`)},
		}),
		i18n.WithPlural("en", rule),
		i18n.WithMissingHandler(func(m i18n.Miss) {
			if m.Reason == i18n.MissingForm {
				mu.Lock()
				keys = append(keys, m.Key)
				mu.Unlock()
			}
		}),
	)
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"app.n.two"}, keys, "zero and other must never be reported as dead")
}

func TestZeroConventionBeatsWiredRule(t *testing.T) {
	t.Parallel()
	// A rule sending 0 to "other", with a catalog that defines "zero": the
	// translator's zero form wins.
	rule := func(n int) i18n.PluralCategory {
		if n == 1 {
			return i18n.One
		}
		return i18n.Other
	}
	b := newBundle(t, i18n.WithPlural("en", rule))
	assert.Equal(t, "Your cart is empty", b.TN(b.Default(), "cart.items", 0))
}

func TestAppendT(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	en := b.Default()
	got := b.AppendT([]byte("x:"), en, "app.title")
	assert.Equal(t, "x:Dashboard", string(got))
	got = b.AppendTN([]byte("x:"), en, "cart.items", 1)
	assert.Equal(t, "x:1 item in your cart", string(got))
}

func TestAppendMissEchoesKey(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	en := b.Default()
	assert.Equal(t, "x:app.nope", string(b.AppendT([]byte("x:"), en, "app.nope")))
	assert.Equal(t, "x:app.nope", string(b.AppendTN([]byte("x:"), en, "app.nope", 2)))
}

func TestTNMissEchoesKey(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	assert.Equal(t, "app.nope", b.TN(b.Default(), "app.nope", 2))
}

func TestTNRendersPlainMessage(t *testing.T) {
	t.Parallel()
	// A key with no plural forms still renders under TN, with count injected —
	// so a catalog may later promote it to plural forms without breaking
	// callers that already say TN.
	b := newBundle(t)
	en := b.Default()
	assert.Equal(t, "Dashboard", b.TN(en, "app.title", 3))
	assert.Equal(t, "x:Dashboard", string(b.AppendTN([]byte("x:"), en, "app.title", 3)))
}

func TestLocaleFromAnotherBundleResolvesByLanguage(t *testing.T) {
	t.Parallel()
	// A Locale is globally meaningful: the same tag means the same thing in
	// every Bundle. One bundle's regional Locale used against a bundle that
	// only carries the base language must layer down to it, not silently
	// become the default.
	regional, err := i18n.New(i18n.WithMessages(fstest.MapFS{
		"en/app.json":    &fstest.MapFile{Data: []byte(`{"k": "en"}`)},
		"pt-BR/app.json": &fstest.MapFile{Data: []byte(`{"k": "pt-BR"}`)},
		"de/app.json":    &fstest.MapFile{Data: []byte(`{"k": "de"}`)},
	}))
	require.NoError(t, err)
	ptBR, err := regional.Parse("pt-BR")
	require.NoError(t, err)
	require.Equal(t, "pt-BR", ptBR.Tag())

	base, err := i18n.New(i18n.WithMessages(fstest.MapFS{
		"en/app.json": &fstest.MapFile{Data: []byte(`{"k": "en"}`)},
		"pt/app.json": &fstest.MapFile{Data: []byte(`{"k": "pt"}`)},
	}))
	require.NoError(t, err)
	assert.Equal(t, "pt", base.T(ptBR, "app.k"))

	// A language the second bundle carries no catalog for at all falls all the
	// way to its default.
	de, err := regional.Parse("de")
	require.NoError(t, err)
	assert.Equal(t, "en", base.T(de, "app.k"))
}

func TestRuleReturningUnknownCategoryDoesNotPanic(t *testing.T) {
	t.Parallel()
	// Rules are caller-supplied, so a bad one must degrade, not crash: an
	// out-of-range category resolves to nothing and the chain carries on.
	junk := func(int) i18n.PluralCategory { return i18n.PluralCategory(200) }
	b := newBundle(t, i18n.WithPlural("uk", junk))
	uk := b.ParseOrDefault("uk")
	assert.NotPanics(t, func() {
		// uk's forms are unreachable via a junk category, so the lookup falls
		// through to en, which resolves with en's own rule.
		assert.Equal(t, "1 item in your cart", b.TN(uk, "cart.items", 1))
	})
}

func TestNewErrors(t *testing.T) {
	t.Parallel()
	t.Run("no messages", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New()
		require.ErrorIs(t, err, i18n.ErrInvalidCatalog)
	})
	t.Run("default locale absent from catalogs", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(
			i18n.WithConfig(i18n.Config{DefaultLocale: "fr", CookieName: "lang", QueryParam: "lang"}),
			i18n.WithMessages(os.DirFS("testdata/locales")),
		)
		require.ErrorIs(t, err, i18n.ErrInvalidCatalog)
	})
	t.Run("invalid default locale", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(i18n.WithConfig(i18n.Config{DefaultLocale: "-"}))
		require.ErrorIs(t, err, i18n.ErrInvalidConfig)
	})
	t.Run("nil plural rule", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(
			i18n.WithMessages(os.DirFS("testdata/locales")),
			i18n.WithPlural("uk", nil),
		)
		require.ErrorIs(t, err, i18n.ErrNilRule)
	})
	t.Run("bad json", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(i18n.WithMessages(fstest.MapFS{
			"en/app.json": &fstest.MapFile{Data: []byte("{oops")},
		}))
		require.ErrorIs(t, err, i18n.ErrInvalidCatalog)
	})
	t.Run("inline translations under an unnormalizable tag", func(t *testing.T) {
		t.Parallel()
		_, err := i18n.New(i18n.WithTranslations("-", "app", map[string]any{"k": "v"}))
		require.ErrorIs(t, err, i18n.ErrInvalidCatalog)
	})
}

// TestNewMergeErrors covers collisions between two sources for one locale.
// Each source is internally consistent, so only the merge can catch these.
func TestNewMergeErrors(t *testing.T) {
	t.Parallel()
	// srcs builds two single-file sources for locale en.
	srcs := func(a, b string) []i18n.Option {
		return []i18n.Option{
			i18n.WithMessages(fstest.MapFS{"en/app.json": &fstest.MapFile{Data: []byte(a)}}),
			i18n.WithMessages(fstest.MapFS{"en/app.json": &fstest.MapFile{Data: []byte(b)}}),
		}
	}
	tests := map[string]struct{ a, b string }{
		"same message key":         {`{"n": "one"}`, `{"n": "two"}`},
		"same plural key":          {`{"n": {"one": "a"}}`, `{"n": {"one": "b"}}`},
		"plural then message":      {`{"n": {"one": "a"}}`, `{"n": "plain"}`},
		"message then plural":      {`{"n": "plain"}`, `{"n": {"one": "a"}}`},
		"plural then form message": {`{"n": {"one": "a"}}`, `{"n.one": "shadow"}`},
		"form message then plural": {`{"n.one": "shadow"}`, `{"n": {"one": "a"}}`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := i18n.New(srcs(tc.a, tc.b)...)
			require.ErrorIs(t, err, i18n.ErrDuplicateKey)
		})
	}
}

// TestNewMergesDistinctSources is the companion to TestNewMergeErrors: two
// sources that do not collide must layer into one catalog.
func TestNewMergesDistinctSources(t *testing.T) {
	t.Parallel()
	b, err := i18n.New(
		i18n.WithMessages(fstest.MapFS{"en/app.json": &fstest.MapFile{Data: []byte(`{"a": "A"}`)}}),
		i18n.WithMessages(fstest.MapFS{"en/other.json": &fstest.MapFile{Data: []byte(`{"b": "B"}`)}}),
	)
	require.NoError(t, err)
	assert.Equal(t, "A", b.T(b.Default(), "app.a"))
	assert.Equal(t, "B", b.T(b.Default(), "other.b"))
}

func TestWithTranslations(t *testing.T) {
	t.Parallel()
	b, err := i18n.New(i18n.WithTranslations("en", "app", map[string]any{
		"title": "Programmatic",
		"nest":  map[string]any{"k": "v"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "Programmatic", b.T(b.Default(), "app.title"))
	assert.Equal(t, "v", b.T(b.Default(), "app.nest.k"))
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	require.NoError(t, i18n.DefaultConfig().Validate())
	require.ErrorIs(t, i18n.Config{DefaultLocale: ""}.Validate(), i18n.ErrInvalidConfig)
	require.ErrorIs(t, i18n.Config{DefaultLocale: "--"}.Validate(), i18n.ErrInvalidConfig)
}

func TestMissReasonString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "missing_key", i18n.MissingKey.String())
	assert.Equal(t, "missing_form", i18n.MissingForm.String())
}

func TestBundleConcurrentReads(t *testing.T) {
	t.Parallel()
	b := newBundle(t)
	en, uk := b.ParseOrDefault("en"), b.ParseOrDefault("uk")
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			assert.Equal(t, "Dashboard", b.T(en, "app.title"))
			assert.Equal(t, "Панель", b.T(uk, "app.title"))
			assert.Equal(t, "1 item in your cart", b.TN(en, "cart.items", 1))
		})
	}
	wg.Wait()
}
