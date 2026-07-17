package i18n

import "io/fs"

// config is the internal accumulator New builds from Options. Options never
// fail; everything fallible (parsing, validation) happens in New, so option
// application stays a plain func(*config) per the repo idiom.
type config struct {
	plurals  map[string]PluralRule
	formats  map[string]FormatSpec
	onMiss   func(Miss)
	cfg      Config
	sources  []fs.FS
	inline   []inlineSource
	nilRules []string // languages passed a nil rule; New turns these into ErrNilRule
}

// inlineSource is one WithTranslations call.
type inlineSource struct {
	data map[string]any
	tag  string
	ns   string
}

// Option configures New.
type Option func(*config)

// WithConfig replaces the whole Config. Without it, New uses DefaultConfig.
func WithConfig(c Config) Option {
	return func(cf *config) { cf.cfg = c }
}

// WithMessages loads catalogs from fsys, laid out as {tag}/{namespace}.json.
// The directory names become the bundle's supported locales — this package
// validates them against no list, so any tag works. May be passed more than
// once; a key defined by two sources is a construction error.
func WithMessages(fsys fs.FS) Option {
	return func(cf *config) { cf.sources = append(cf.sources, fsys) }
}

// WithTranslations adds one locale's namespace programmatically, equivalent to
// a {tag}/{ns}.json file. data may nest; it is flattened to dot notation.
func WithTranslations(tag, ns string, data map[string]any) Option {
	return func(cf *config) {
		cf.inline = append(cf.inline, inlineSource{tag: tag, ns: ns, data: data})
	}
}

// WithPlural wires a plural rule for one base language ("uk", not "uk-UA" —
// plural grammar does not vary by region; a regional tag is reduced to its
// language). Locales with no wired rule use DefaultRule.
//
// This package ships no per-language rules. Correct CLDR rules live in
// core/i18n/cldr: i18n.WithPlural("uk", cldr.Uk).
func WithPlural(lang string, rule PluralRule) Option {
	return func(cf *config) {
		// A nil rule is a programmer error whatever the tag says, so it is
		// reported ahead of the tag check: New turns nilRules into ErrNilRule.
		if rule == nil {
			cf.nilRules = append(cf.nilRules, lang)
			return
		}
		if l := newLocale(lang).Lang(); l != "" {
			cf.plurals[l] = rule
		}
	}
}

// WithFormat wires a FormatSpec for one locale tag. Resolution is tag, then
// base language, then Invariant.
//
// This package ships no per-locale specs. Correct CLDR specs live in
// core/i18n/cldr: i18n.WithFormat("es-MX", cldr.FormatEsMX).
func WithFormat(tag string, spec FormatSpec) Option {
	return func(cf *config) {
		if t := normalizeTag(tag); t != "" {
			cf.formats[t] = spec
		}
	}
}

// WithMissingHandler sets the hook called for a missing key at render time and
// for incomplete or dead plural forms at construction. Nil by default.
//
// The handler runs on the render path: it must be fast and non-blocking.
// Offload heavy work (logging to a remote sink, DB writes) to a channel. It
// may be called concurrently, since T and TN are safe for concurrent use.
func WithMissingHandler(h func(Miss)) Option {
	return func(cf *config) { cf.onMiss = h }
}
