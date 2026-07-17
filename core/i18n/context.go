package i18n

import (
	"context"
	"time"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/money"
)

// localizerKey carries the request's Localizer. The value is a Localizer, not
// a Locale, so it brings its own Bundle — which is what lets the package-level
// one-liners work without a package-global bundle, and lets two bundles
// coexist in one process.
var localizerKey = ctxkey.New[Localizer]("i18n.localizer")

// WithLocale stamps a Localizer for loc into ctx. Middleware does this per
// request; background jobs do it explicitly:
//
//	ctx = bundle.WithLocale(ctx, bundle.ParseOrDefault(user.Locale))
//
// after which the package-level one-liners work exactly as in a handler.
func (b *Bundle) WithLocale(ctx context.Context, loc Locale) context.Context {
	return localizerKey.With(ctx, b.For(loc))
}

// Ctx returns the Localizer in ctx, or one bound to this bundle's default
// locale. Unlike FromContext it always returns a usable Localizer, because it
// has a bundle to fall back to.
func (b *Bundle) Ctx(ctx context.Context) Localizer {
	if l, ok := localizerKey.From(ctx); ok {
		return l
	}
	return b.For(b.Default())
}

// FromContext returns the Localizer stamped into ctx, or the zero Localizer.
// The zero value fails closed — messages echo their keys, values format
// invariantly — so callers never need to check.
func FromContext(ctx context.Context) Localizer {
	l, _ := localizerKey.From(ctx)
	return l
}

// LocaleFrom returns the locale in ctx, or the zero Locale. Useful for a
// logger.ContextExtractor: i18n.LocaleFrom(ctx).Tag().
func LocaleFrom(ctx context.Context) Locale { return FromContext(ctx).Locale() }

// T renders a message in the context's locale.
func T(ctx context.Context, key string, args ...any) string {
	return FromContext(ctx).T(key, args...)
}

// TN renders a pluralized message in the context's locale, injecting n as
// {{count}}.
func TN(ctx context.Context, key string, n int, args ...any) string {
	return FromContext(ctx).TN(key, n, args...)
}

// TK is T with a declared Key.
func TK(ctx context.Context, k Key, args ...any) string {
	return FromContext(ctx).TK(k, args...)
}

// TNK is TN with a declared Key.
func TNK(ctx context.Context, k Key, n int, args ...any) string {
	return FromContext(ctx).TNK(k, n, args...)
}

// Number formats v in the context's locale.
func Number(ctx context.Context, v float64) string { return FromContext(ctx).Number(v) }

// NumberInt formats n in the context's locale.
func NumberInt(ctx context.Context, n int64) string { return FromContext(ctx).NumberInt(n) }

// Percent formats a ratio in the context's locale.
func Percent(ctx context.Context, ratio float64) string { return FromContext(ctx).Percent(ratio) }

// Currency formats m in the context's locale.
func Currency(ctx context.Context, m money.Money) string { return FromContext(ctx).Currency(m) }

// Date formats t's date in the context's locale.
func Date(ctx context.Context, t time.Time) string { return FromContext(ctx).Date(t) }

// Time formats t's clock time in the context's locale.
func Time(ctx context.Context, t time.Time) string { return FromContext(ctx).Time(t) }

// DateTime formats t in the context's locale.
func DateTime(ctx context.Context, t time.Time) string { return FromContext(ctx).DateTime(t) }
