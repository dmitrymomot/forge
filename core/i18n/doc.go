// Package i18n provides internationalization as pure mechanism: message
// catalogs, locale negotiation, and locale-aware formatting, with no
// knowledge of any actual language.
//
// The package ships no list of locales, no per-language plural rules, and no
// per-locale number or date formats. The set of supported locales is
// whatever your catalogs declare — ship a vi/ directory and Vietnamese
// works, with no change to this package. A locale with no wired plural rule
// uses DefaultRule (zero/one/many); a locale with no wired format uses
// Invariant (ISO-8601, "." decimal). Neither is an error, and neither
// pretends to know your language's grammar.
//
// Correct CLDR data lives in the sibling package core/i18n/cldr and is
// never applied automatically:
//
//	bundle, err := i18n.New(
//	    i18n.WithMessages(catalogFS),
//	    i18n.WithPlural("uk", cldr.Uk),
//	    i18n.WithFormat("uk-UA", cldr.FormatUkUA),
//	)
//
// WithFormat's tag must match a real locale tag in your catalog: FormatUkUA
// applies to a uk-UA locale (resolved tag → base language → Invariant) and
// will not apply to a bare uk locale, even though WithPlural's base-language
// key does.
//
// # Catalogs
//
// Catalogs are JSON under an fs.FS, laid out as {tag}/{namespace}.json. The
// namespace becomes the leading key segment and nested objects flatten to
// dot notation, so en/app.json's {"buttons": {"save": "Save"}} is
// app.buttons.save.
//
//	//go:embed locales
//	var localesFS embed.FS
//
//	sub, _ := fs.Sub(localesFS, "locales")
//	bundle, err := i18n.New(i18n.WithMessages(sub))
//
// # Lookup and fallback
//
// Resolution is exact tag, then base language, then the default locale:
// en-GB falls back to en, and es-MX to es, then to the default. The same
// chain applies per key, so a partially translated regional catalog layers
// over its base language.
//
// # Plurals
//
// A JSON object whose keys are all CLDR category names is a plural message:
//
//	{"items": {"zero": "Cart is empty", "one": "1 item", "other": "{{count}} items"}}
//
// TN selects the form with the locale's rule and injects the count as
// {{count}}. A missing form falls back along two→few→many→other,
// few→many→other, zero/one/many→other. If the catalog defines a zero form,
// it wins at n == 0 regardless of the rule — a convenience for translators.
//
// # HTTP
//
// Middleware resolves the locale (cookie → query → Accept-Language →
// default) and stamps a Localizer into the request context:
//
//	mux.Handle("/", bundle.Middleware()(handler))
//
// after which the package-level one-liners work anywhere downstream:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//	    title := i18n.T(r.Context(), "app.title")
//	    price := i18n.Currency(r.Context(), amount)
//	}
//
// Background jobs stamp the context themselves and use the identical calls:
//
//	ctx = bundle.WithLocale(ctx, bundle.ParseOrDefault(user.Locale))
//
// A context with no Localizer fails closed: messages echo their key and
// values format invariantly. Nothing panics.
//
// # Templates
//
// Localizer is a two-word value with the full method set and the locale
// already resolved. Embed it in html/template data:
//
//	tmpl.Execute(w, map[string]any{"L": bundle.Ctx(r.Context())})
//	// {{ .L.T "app.title" }}
//
// # Relative time
//
// This package has no relative-time helper. "3 hours ago" is a threshold
// ladder — where "just now" ends, whether day one reads "yesterday" — and
// those are product decisions. Express it with plurals, one key per unit:
//
//	{"time_ago": {"minutes": {"zero": "just now", "one": "a minute ago",
//	                          "other": "{{count}} minutes ago"}}}
//
// then pick the unit and call TN.
//
// # Concurrency
//
// A Bundle is immutable after New and safe for concurrent use. Rendering
// never returns an error and never panics.
package i18n
