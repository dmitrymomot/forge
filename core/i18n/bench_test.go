package i18n_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/i18n"
	"github.com/dmitrymomot/forge/core/money"
)

// Sinks every benchmark writes its result into, so the compiler cannot prove
// the call's result is unused and dead-code-eliminate it. b.Loop() already
// keeps loop-body results alive via its own KeepAlive transform, but writing
// through a package-level var makes that guarantee explicit and independent
// of the loop idiom.
var (
	sink       string
	sinkB      []byte
	sinkLocale i18n.Locale
	sinkBundle *i18n.Bundle
)

// benchBundle wires the same testdata/locales fixture newBundle, fmtBundle and
// curBundle already use (bundle_test.go, format_test.go, currency_test.go),
// plus deSpec (declared in format_test.go) on "de" so the formatting,
// currency and date/time benchmarks exercise a wired FormatSpec rather than
// Invariant. Those three helpers all take *testing.T, not *testing.B, so this
// is its own minimal builder rather than a fourth near-duplicate.
func benchBundle(b *testing.B) *i18n.Bundle {
	b.Helper()
	bundle, err := i18n.New(
		i18n.WithMessages(os.DirFS("testdata/locales")),
		i18n.WithFormat("de", deSpec),
	)
	require.NoError(b, err)
	return bundle
}

// benchKeyGreeting is app.greeting as a declared Key. key_test.go already
// declares keyTitle/keyItems/keyTypo, but nothing for the arg-taking greeting
// message that BenchmarkTK needs in order to compare against BenchmarkT.
var benchKeyGreeting = i18n.NewKey("app.greeting")

func BenchmarkT(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.Default()
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.T(loc, "app.greeting", "name", "Ann")
	}
}

// BenchmarkTK answers whether the message-key map lookup is worth removing.
// Compare against BenchmarkT: TK currently just delegates to T (no cache), so
// the two should be near-identical — that delta (or lack of one) is Task 18's
// evidence for whether a cache is worth adding.
func BenchmarkTK(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.Default()
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.TK(loc, benchKeyGreeting, "name", "Ann")
	}
}

// BenchmarkTLiteral isolates the no-args, no-substitution path.
func BenchmarkTLiteral(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.Default()
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.T(loc, "app.title")
	}
}

// BenchmarkTViaLocalizer measures the ctx/template path: the locale index is
// already resolved, so this is the cheapest layer.
func BenchmarkTViaLocalizer(b *testing.B) {
	bundle := benchBundle(b)
	l := bundle.For(bundle.Default())
	b.ReportAllocs()
	for b.Loop() {
		sink = l.T("app.greeting", "name", "Ann")
	}
}

func BenchmarkTViaContext(b *testing.B) {
	bundle := benchBundle(b)
	ctx := bundle.WithLocale(context.Background(), bundle.Default())
	b.ReportAllocs()
	for b.Loop() {
		sink = i18n.T(ctx, "app.greeting", "name", "Ann")
	}
}

func BenchmarkAppendT(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.Default()
	dst := make([]byte, 0, 128)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendT(dst[:0], loc, "app.greeting", "name", "Ann")
	}
	sinkB = dst
}

func BenchmarkTN(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.Default()
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.TN(loc, "cart.items", 5)
	}
}

// BenchmarkTNFallback measures the deep path: de has no cart.json at all, so
// the plural lookup resolves all the way through to the default locale's
// catalog.
func BenchmarkTNFallback(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.TN(loc, "cart.items", 5)
	}
}

func BenchmarkAppendTN(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.Default()
	dst := make([]byte, 0, 64)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendTN(dst[:0], loc, "cart.items", 5)
	}
	sinkB = dst
}

func BenchmarkNumber(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.Number(loc, 1234567.89)
	}
}

func BenchmarkAppendNumber(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	dst := make([]byte, 0, 64)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendNumber(dst[:0], loc, 1234567.89)
	}
	sinkB = dst
}

func BenchmarkNumberInt(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.NumberInt(loc, 1234567)
	}
}

func BenchmarkAppendNumberInt(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	dst := make([]byte, 0, 32)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendNumberInt(dst[:0], loc, 1234567)
	}
	sinkB = dst
}

func BenchmarkPercent(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.Percent(loc, 0.5)
	}
}

func BenchmarkAppendPercent(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	dst := make([]byte, 0, 16)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendPercent(dst[:0], loc, 0.5)
	}
	sinkB = dst
}

// BenchmarkCurrency reuses eur, declared in currency_test.go, rather than a
// second identical Currency literal.
func BenchmarkCurrency(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	m := money.FromMinor(123456, eur)
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.Currency(loc, m)
	}
}

// BenchmarkAppendCurrency's allocs/op floor is money's own decimal arithmetic
// (see TestAppendCurrencyAddsNoAllocsOverMoney in currency_test.go), not zero —
// this benchmark just reports the number; the unit test already asserts the
// falsifiable "adds nothing over money's floor" claim.
func BenchmarkAppendCurrency(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	m := money.FromMinor(123456, eur)
	dst := make([]byte, 0, 64)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendCurrency(dst[:0], loc, m)
	}
	sinkB = dst
}

func BenchmarkDate(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.Date(loc, ts)
	}
}

func BenchmarkAppendDate(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	dst := make([]byte, 0, 32)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendDate(dst[:0], loc, ts)
	}
	sinkB = dst
}

func BenchmarkTime(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.Time(loc, ts)
	}
}

func BenchmarkAppendTime(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	dst := make([]byte, 0, 16)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendTime(dst[:0], loc, ts)
	}
	sinkB = dst
}

func BenchmarkDateTime(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		sink = bundle.DateTime(loc, ts)
	}
}

func BenchmarkAppendDateTime(b *testing.B) {
	bundle := benchBundle(b)
	loc := bundle.ParseOrDefault("de")
	ts := time.Date(2026, 7, 17, 15, 4, 5, 0, time.UTC)
	dst := make([]byte, 0, 40)
	b.ReportAllocs()
	for b.Loop() {
		dst = bundle.AppendDateTime(dst[:0], loc, ts)
	}
	sinkB = dst
}

func BenchmarkNegotiate(b *testing.B) {
	bundle := benchBundle(b)
	b.ReportAllocs()
	for b.Loop() {
		sinkLocale = bundle.Negotiate("de-AT,de;q=0.9,en-US;q=0.8,en;q=0.7")
	}
}

func BenchmarkMiddleware(b *testing.B) {
	bundle := benchBundle(b)
	h := bundle.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "de-AT,de;q=0.9,en;q=0.8")
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkNew(b *testing.B) {
	fsys := os.DirFS("testdata/locales")
	b.ReportAllocs()
	for b.Loop() {
		bundle, err := i18n.New(i18n.WithMessages(fsys), i18n.WithFormat("de", deSpec))
		if err != nil {
			b.Fatal(err)
		}
		sinkBundle = bundle
	}
}
