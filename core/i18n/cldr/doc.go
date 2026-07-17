// Package cldr carries CLDR plural rules and locale format specs for
// core/i18n.
//
// It is data, not policy. core/i18n imports nothing from this package and
// applies none of it automatically — the dependency arrow runs one way, and
// the compiler enforces it. Wire what you want, explicitly:
//
//	bundle, err := i18n.New(
//	    i18n.WithMessages(catalogFS),
//	    i18n.WithPlural("uk", cldr.Uk),
//	    i18n.WithPlural("pl", cldr.Pl),
//	    i18n.WithFormat("uk-UA", cldr.FormatUkUA),
//	)
//
// A locale you do not wire is not broken: core/i18n falls back to its
// zero-one-many DefaultRule and to Invariant formatting.
//
// # Keys
//
// Plural rules are keyed by base language (Uk, Pl, Ar) — plural grammar
// does not vary by region. Format specs are keyed by locale tag (FormatEsMX,
// FormatEsES) — es-MX and es-ES pluralize identically and format nothing
// alike.
//
// This means a format spec's tag must match your catalog's locale tag: wiring
// FormatUkUA only takes effect for a uk-UA locale/catalog (WithFormat resolves
// tag → base language → Invariant) — a bare uk locale falls through to
// Invariant formatting even though WithPlural("uk", cldr.Uk) still applies.
//
// # No family buckets
//
// Every language has its own rule, even where two share an implementation.
// Bucketing languages by family is how "Slavic" rules made Ukrainian 21 read
// as many (CLDR says one) and "Romance" rules made Portuguese 0 read as
// other (CLDR says one). Uk and Ru share a body; they do not share an
// identifier.
//
// # Verification
//
// Every rule is differential-tested against golang.org/x/text's
// CLDR-generated tables, with a documented exception list where x/text's
// vintage is stale (notably the Romance whole-millions "many" added in
// CLDR 38). x/text is a test-only dependency and appears in no shipped code.
package cldr
