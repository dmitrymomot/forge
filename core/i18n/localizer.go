package i18n

import (
	"time"

	"github.com/dmitrymomot/forge/core/money"
)

// Localizer is a locale-bound view of a Bundle: the full method set with the
// locale already resolved. It is two machine words — a *Bundle pointer and a
// cached locale index — so it is cheap to copy, safe to embed in html/template
// data ({{ .L.T "dashboard.title" }}) and to carry in a context.
//
// It caches the resolved locale index, so its lookups skip the tag hash the
// explicit Bundle methods pay on every call.
//
// The zero Localizer is valid and fails closed: messages echo their key and
// values format with Invariant. That is what a request which never passed
// through Middleware yields, and it must never panic.
type Localizer struct {
	b   *Bundle
	idx int
}

// For binds a locale to the bundle, resolving it once.
func (b *Bundle) For(loc Locale) Localizer {
	return Localizer{b: b, idx: b.locIdx(loc)}
}

// IsZero reports whether the Localizer is unbound.
func (l Localizer) IsZero() bool { return l.b == nil }

// Locale returns the resolved locale; the zero Locale when unbound.
func (l Localizer) Locale() Locale {
	if l.b == nil {
		return Locale{}
	}
	return Locale{tag: l.b.locales[l.idx].tag}
}

// spec returns the bound locale's FormatSpec, or Invariant when unbound.
func (l Localizer) spec() *FormatSpec {
	if l.b == nil {
		return &Invariant
	}
	return l.b.specFor(l.idx)
}

// T renders a message.
func (l Localizer) T(key string, args ...any) string {
	if l.b == nil {
		return key
	}
	return l.b.tAt(l.idx, key, args...)
}

// TN renders a pluralized message, injecting n as {{count}}.
func (l Localizer) TN(key string, n int, args ...any) string {
	if l.b == nil {
		return key
	}
	return l.b.tnAt(l.idx, key, n, args...)
}

// TK is T with a declared Key.
func (l Localizer) TK(k Key, args ...any) string { return l.T(k.s, args...) }

// TNK is TN with a declared Key.
func (l Localizer) TNK(k Key, n int, args ...any) string { return l.TN(k.s, n, args...) }

// Number formats v with the locale's separators.
func (l Localizer) Number(v float64) string {
	return string(appendNumberSpec(make([]byte, 0, 32), v, l.spec()))
}

// NumberInt formats n with the locale's grouping separator.
func (l Localizer) NumberInt(n int64) string {
	return string(appendIntSpec(make([]byte, 0, 24), n, l.spec()))
}

// Percent formats a ratio as a percentage.
func (l Localizer) Percent(ratio float64) string {
	return string(appendPercentSpec(make([]byte, 0, 16), ratio, l.spec()))
}

// Currency formats m: amount and symbol from money, separators and placement
// from the locale.
func (l Localizer) Currency(m money.Money) string {
	return string(appendCurrencySpec(make([]byte, 0, 32), m, l.spec()))
}

// Date formats t's date. t renders in the location it carries.
func (l Localizer) Date(t time.Time) string {
	return string(t.AppendFormat(make([]byte, 0, 32), l.spec().DateLayout))
}

// Time formats t's clock time.
func (l Localizer) Time(t time.Time) string {
	return string(t.AppendFormat(make([]byte, 0, 16), l.spec().TimeLayout))
}

// DateTime formats t's date and time.
func (l Localizer) DateTime(t time.Time) string {
	return string(t.AppendFormat(make([]byte, 0, 40), l.spec().DateTimeLayout))
}
