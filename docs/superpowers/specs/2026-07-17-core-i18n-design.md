# core/i18n — single-package internationalization

Date: 2026-07-17. Status: approved design, pre-plan. Revision 2 (2026-07-17): mechanism/policy split — see "Revision history".

## Decision

Build one package, `core/i18n`, replacing the four planned packages `i18n/catalog`, `i18n/locale`, `i18n/numbers`, `i18n/dates`, plus one data-only sibling `core/i18n/cldr`. Rationale: in real apps (SaaS, iGaming) translation, locale negotiation, and number/currency/date formatting are always used together; splitting them spread one feature across four imports, forced plural rules and a shared `Locale` type into an awkward home, and duplicated CLDR data between `numbers` and `dates`. The single package welds them deliberately and provides one-line ctx-based calls everywhere (handlers, templ, background jobs).

**`core/i18n` is mechanism; it knows no languages.** It ships no list of locales, no per-language plural rules, and no per-locale format data. The set of supported locales is whatever the application's catalogs declare. Correct CLDR data lives in `core/i18n/cldr`, is never imported by core, and is never applied automatically — you wire it explicitly or you don't get it. This is the load-bearing decision of the design; everything below follows from it.

Heritage: this is the third iteration of the author's i18n design (boilerplate `pkg/i18n` → old-forge `pkg/i18n` → this). It keeps the proven ideas (immutable-after-New, flattened dot-notation catalogs, plural sub-keys with form fallback, language fallback chain, Translator/Localizer pattern, hardened Accept-Language parsing) and fixes the known flaws (per-call composite-key allocation, family-bucket plural bugs, float64 money, locale/format mismatch footgun, YAML dep, and — new in revision 2 — the framework deciding which languages an application may support).

## Package shape

- Path `core/i18n`, package `i18n`. Deps: `core/money`, `core/ctxkey`. No x/text, no YAML, no logger dep, no external deps.
- Path `core/i18n/cldr`, package `cldr`. Deps: `core/i18n` only. **The dependency arrow points one way and the compiler enforces it: `cldr` imports `i18n`; `i18n` never imports `cldr`.** An application that does not import `cldr` links none of its data.
- Idiom: `New(opts ...Option) (*Bundle, error)`; env-loadable `Config` (`I18N_DEFAULT_LOCALE`, `I18N_COOKIE_NAME`, `I18N_QUERY_PARAM`) + `DefaultConfig()` + `Validate()`, applied via `WithConfig` (design.md's `New(...Option)` idiom — the reference implementation's `New(cfg, opts...)` shape is not the forge convention).
- Anatomy: `doc.go` (runnable example), `config.go`, `options.go`, `errors.go`, impl files, `bench_test.go`. `core/i18n` contains **no data tables**; `cldr` is almost entirely data tables.
- LOC guideline note: hand-written logic stays within the 250–850 band; `cldr`'s curated data tables are excluded from the count (precedent: `core/country`'s 249 vars).
- Tenancy: locale is a passed value, not a scope — no `WithScope` seam (precedent: `core/country`). Per-tenant translation overrides, if ever needed, are separate `Bundle` instances constructed per tenant.

## Core types

- **`Locale`** — an immutable, comparable value wrapping a normalized BCP-47-style tag: `struct{ tag string }`, with `Tag() string` (`"pt-BR"`), `Lang() string` (`"pt"`), `String() string`, `IsZero() bool`. Normalization happens at the boundary (`Parse`/negotiation), so `"EN"`, `"en_US"`, and `" en-US "` cannot fragment a catalog. A `Locale` is globally meaningful: the same tag means the same thing in every bundle. The zero `Locale` is invalid and behaves as "unresolved" — every consumer falls back to the bundle default (fail-closed, never panics).
- **`Bundle`** — the immutable engine built by `New`: per-locale flattened message tables of precompiled segments, the plural rule and `FormatSpec` for each locale, default locale, supported set. Safe for concurrent use; nothing mutates after `New`.
- **`Localizer`** — a two-word value `{*Bundle, localeIdx}` returned by `bundle.Ctx(ctx)` or `bundle.For(loc)`; the locale-bound view with the full method set. It caches the resolved locale index, so its lookups are array indexing. This is what travels in context and what `html/template` data embeds.
- **`Key`** — a pre-resolved message-key handle for hot paths (`var titleKey = i18n.NewKey("dashboard.title")` at package-var level). It holds the key string plus an atomically-cached `{bundle, index}` pair resolved on first use against a given bundle; a subsequent `TK` on that same bundle is an array index, and a `Key` used against a *different* bundle simply falls back to the map lookup (i.e. degrades to string-keyed `T`'s cost, never to a wrong answer). Keys are package-level shared state, so the cache must be atomic. Validation is explicit via `Bundle.ValidateKeys(keys ...Key) error`, **not** a package-global registry: a global registry would break binaries constructing multiple bundles over different catalogs, and validation is the one thing that must not silently pass for the wrong catalog.
- **`PluralRule`** — `func(n int) PluralCategory`, with `PluralCategory` a dense enum (`Zero, One, Two, Few, Many, Other`). The custom-rule seam.
- **`FormatSpec`** — a plain struct of one locale's rendering conventions (separators, currency placement, percent spacing, Go date layouts). The custom-format seam. Core defines the type and the engines that read it; core ships no instances of it beyond the invariant default.

## Construction

```go
bundle, err := i18n.New(
    i18n.WithConfig(cfg),                       // or rely on DefaultConfig
    i18n.WithMessages(localesFS),               // fs.FS, JSON only, embed-friendly
    i18n.WithTranslations("en", "app", m),      // or programmatic, same effect
    i18n.WithPlural("uk", cldr.Uk),             // opt-in; nothing is automatic
    i18n.WithFormat("es-MX", cldr.FormatEsMX),  // opt-in; nothing is automatic
    i18n.WithMissingHandler(onMiss),            // func(i18n.Miss), nil default = no-op
)
```

- **Loading**: `{lang}/{namespace}.json` under the fs.FS root; nested JSON flattened to dot notation; the namespace becomes the leading key segment (`email.invite.subject`) — no namespace parameter anywhere in the read API. Duplicate keys across files = construction error.
- **Precompilation**: every message is parsed once at `New` into literal/placeholder segments (`{{name}}` syntax); rendering is a single pass with zero re-parsing. No in-flight file reads ever; catalogs are final at construction.
- **Supported set**: the union of locales declared by the catalogs (`WithMessages` directory names, `WithTranslations` language arguments), normalized. Any tag is permitted — `vi`, `sw`, `ww-WW` — because the application, not the framework, decides what it supports. The default locale must be among them, or `New` fails. `Bundle.Locales()` exposes the set (drives negotiation and e.g. a language-switcher UI).
- **Validation at `New`**: unparseable JSON, empty catalogs, a default locale with no messages, and a nil plural rule are construction errors. An unconfigured plural rule is **not** an error — the built-in default applies.
- **Plural-form probing** (non-fatal, via the miss handler with `Reason: MissingForm`): for each plural message, `SupportedForms(rule)` is compared against the forms the catalog defines, and **both** directions are reported — a form the catalog defines that the language's rule can never produce (dead translation), and a form the rule does produce that the catalog lacks (incomplete translation). Probing reports; it never fails construction.

## Locale resolution and fallback

One chain, used for both negotiation and message lookup: **exact tag → base language → default locale**.

- `en-GB` resolves to `en-GB` if the catalogs declare it; otherwise to `en` if declared; otherwise to the default.
- With default `en` and no `es-MX` catalog: `es-MX` → `es` if declared → otherwise `en`.
- An unknown or malformed tag (`ww-WW`, `""`, garbage) is never an error at the HTTP boundary; it resolves to the default. `Parse` returns `ErrUnknownLocale` for callers that want to distinguish; `ParseOrDefault` never fails.

Message lookup applies the same chain per key, so a partially-translated regional catalog layers over its base language: a key missing from `es-MX` falls back to `es`, then to the default, then echoes the key and notifies the miss handler.

## API surface — three layers

1. **Package-level ctx one-liners** (the 99% path; works in handlers, templ, jobs): `i18n.T(ctx, key, args...)`, `i18n.TN(ctx, key, n, args...)`, `i18n.TK/TNK(ctx, Key, ...)`, `i18n.Number(ctx, v)`, `i18n.Currency(ctx, money.Money)`, `i18n.Percent(ctx, v)`, `i18n.Date/Time/DateTime(ctx, t)`, `i18n.LocaleFrom(ctx) Locale`. They read the `Localizer` stamped into ctx by the middleware or `Bundle.WithLocale`; if absent they fail closed: key echo for messages, invariant formatting, never a panic.
2. **`Localizer`** (`bundle.Ctx(ctx)` / `bundle.For(loc)`): same method set minus the ctx parameter; embed in `html/template` render data (`{{ .L.T "dashboard.title" }}`), pass to batch rendering.
3. **Explicit `Bundle` methods** (`bundle.T(loc, key, ...)` etc.): no ctx anywhere; tests and multi-locale batch work.

Placeholder args are variadic key/value pairs: `i18n.T(ctx, "email.invite.subject", "name", inviter)` — one slice alloc, no map. `TN` auto-injects `count`.

Background jobs: `loc := bundle.ParseOrDefault(user.Locale)` then `ctx = bundle.WithLocale(ctx, loc)` — after that the same one-liners and templ components work identically to the HTTP path. Unknown recipient locale = empty string = default locale, a first-class path.

## Messages, plurals, fallback

- **Miss handler seam**: `WithMissingHandler(func(m Miss))` with `Miss{Locale, Key, Reason}` (`MissingKey` at runtime, `MissingForm` from load-time probing). Nil by default — a single nil check, zero overhead. More flexible than a logger dep: consumers log, notify, or record to DB. Documented contract: must be fast and non-blocking (it runs on the render path — offload heavy work to a channel/queue).
- **Plural sub-keys**: `"count": {"one": "...", "few": "...", "many": "...", "other": "..."}`.
- **Form fallback chain** when a translated form is missing: `two→few→many→other`, `few→many→other`, `zero→other`, `many→other`, `one→other`. `other` is the terminal form.
- **The built-in default rule is zero-one-many**: `n == 0 → Zero`, `|n| == 1 → One`, otherwise `Many`. It applies to every locale for which no rule was wired, and it is deliberately not any real language's rule. It composes with the form-fallback chain to behave correctly for a plain `{one, other}` catalog (`Zero`→`other`, `Many`→`other`) while giving a catalog that defines `zero` a working zero form for free. It is a mechanism default, not a claim about anyone's grammar.
- **Per-language CLDR rules are opt-in**: `WithPlural(lang, rule)` wires one language. Rules are keyed by base language (`uk`, not `uk-UA`) — plural grammar does not vary by region. Correct rules ship in `core/i18n/cldr`; a hand-written rule is equally welcome.
- **Zero convention** (opt-in, per message): at `n == 0`, if the catalog defines a `zero` form it wins over the rule's answer. UX affordance for translators ("Your cart is empty"), applied uniformly whether the rule is the built-in default or a wired CLDR rule.
- **validate bridge**: `core/i18n` does NOT import `core/validate`. `validate.Violation{Key, Params}` maps onto `T(loc, v.Key, pairs...)` with a trivial loop; `view/form` (which deps both) owns that adapter. `Localizer.T`'s signature is designed so the adapter is ~5 lines.

## Formatting

Core owns the **engines**; the application owns the **data**.

- **`FormatSpec`**: decimal separator, grouping separator, 3-digit grouping (Indian grouping = icebox), percent spacing, currency symbol placement + spacing, date/time/datetime layouts (Go layouts). Wired per locale tag via `WithFormat(tag, spec)`.
- **Invariant default**: a locale with no wired spec formats with `.` decimal separator, `,` grouping separator, symbol-before currency, and ISO-8601 date layouts. Total, predictable, and obviously neutral — not a claim that any locale looks like this.
- Formats are selected by the same `Locale` value as messages — the old mismatch footgun (German text with US dates) is structurally impossible.
- **`Currency(loc, money.Money)`**: amount from `money.Decimal` rounded to the currency's `MinorUnits`; symbol from `money.Currency.Symbol`; the locale contributes separators and symbol placement only. Multi-currency correct: a de-DE user viewing USD sees `1.234,56 $`. No float64 money anywhere.
- **`Number`** takes float64 plus an int64 variant (`NumberInt`); **`Percent`** takes a ratio (0.5 → `50 %` per locale).
- **Gregorian only.** Date/time layouts are per-locale values, not a user-composable pattern language.
- **No relative time.** Relative time ("3 hours ago") is a threshold ladder — where "just now" ends, whether day 1 reads "yesterday", when to switch to an absolute date — and those are product decisions, not i18n decisions. It is expressible today with the plural mechanism and no new API: one plural key per unit (`time_ago.minutes`, `time_ago.hours`), the application picks the unit and calls `TN`. This also removes the need for per-direction unit overrides that inflected languages (e.g. Czech past-instrumental vs future-accusative) would otherwise force into the type system — they are simply different keys.

## The cldr package

`core/i18n/cldr` is data, not logic. It exists because correct CLDR rules are worth having and impossible to guess; it is a library, never a policy.

- **Plural rules, keyed by base language** (~40): `cldr.En`, `cldr.Uk`, `cldr.Pl`, `cldr.Ar`, … Scope: European (en, de, nl, sv, da, nb, is · fr, es, it, pt, ro, ca, gl · ru, uk, be, pl, cs, sk, sl, hr, sr, bg, mk · lv, lt · fi, et, hu · el, sq, tr, eu) and Asian/Middle-Eastern (ja, zh, ko, th, vi, id, ms, hi · ar, he). Deliberately excluded: `ga`, `cy`, `mt` — 5–6 forms with baroque rules, high verification cost, negligible reach. `cldr.PluralFor(lang) (PluralRule, bool)` offers a lookup for callers that want it; it is a convenience for the caller, never invoked by core.
- **One entry per language, no family buckets.** Family bucketing is what produced the historical `uk`/`ru` "21 → one" bug (`pl,ru,cs,uk,hr,sr,sk,sl,bg` sharing one "Slavic" rule) and the `pt`/`it` "0 → other" bug (`it,pt` sharing one "Romance" rule; CLDR `pt` is `one: i = 0..1`, `it` is not). `cldr.Uk` and `cldr.Pl` are separate because 21 is `one` in Ukrainian and `many` in Polish.
- **Format specs, keyed by locale tag**: `cldr.FormatEnUS`, `cldr.FormatDeDE`, `cldr.FormatEsMX`, `cldr.FormatPtBR`, … Note the key spaces genuinely differ: plural rules are per-language (there is no `es-MX` plural rule — LatAm Spanish pluralizes exactly like Spain), while format specs are per-tag (`es-MX` renders `$1,234.56` where `es-ES` renders `1.234,56 €`). LatAm coverage is therefore a formats concern: es-MX, es-AR, es-CO, es-CL, pt-BR, alongside es-ES and pt-PT.
- **Verification strategy**: every plural rule is differential-tested against `golang.org/x/text/feature/plural`'s CLDR-generated tables over a wide integer probe, with an explicit, documented exception list for cases where x/text is stale — notably the Romance whole-millions `many` category added in CLDR 38 (2020), which x/text v0.38 predates. This makes ~40 rules mechanical to verify rather than 40 acts of faith. x/text is a **test-only** dependency of `cldr`; it must not appear in non-test code.
- Group separators for fr, pl, cs, uk, ru and others are U+00A0 NO-BREAK SPACE, per CLDR. This is intentional and must be written as a visible ` ` escape in Go source, never a raw invisible byte.

## HTTP integration

- `bundle.Middleware(resolvers...)` resolves the request locale via a resolver chain — defaults to cookie (`Config.CookieName`) → query (`Config.QueryParam`) → `Accept-Language` → default; explicit resolvers (`i18n.FromCookie(name)`, `i18n.FromQuery(name)`, `i18n.FromAcceptLanguage()`, `i18n.FromHeader(name)`) override the chain. It stamps the `Localizer` into ctx (single ctx key via `core/ctxkey`), sets `Content-Language`, and appends `Vary: Accept-Language` when the header participated.
- Accept-Language parser ports the old hardening: 4 KB header cap, a cap on parsed tags, q-value validation (0..1, NaN rejected), quality-descending sort, server-preference tie-break on equal q (RFC 7231 §5.3.1), base-language partial matching, `*` resolves to the default locale. Negotiation runs against the bundle's supported set — the set the application declared, not a framework list.
- The middleware never writes cookies; persisting a user's choice is the consumer's concern (anti-scope).
- No logger integration exported: consumers wanting locale in log attrs wire a one-line `logger.ContextExtractor` over `i18n.LocaleFrom(ctx)` in their own logger config.

## Performance

- The hot path is ctx read → array index → plural func → segment append. The `Localizer` caches its locale index, so the ctx and template layers never hash a locale; only the explicit `Bundle.T(loc, ...)` layer pays a tag lookup, and it is the least-used layer.
- String-keyed `T` keeps one map lookup for the message key; `Key` handles remove it via a per-bundle resolved index.
- Precompiled segments render in one pass into a buffer sized from the compiled length hint; target ≤2 allocs per `T` (result string + args slice), 0 extra for `Append` variants.
- Formatters are append-first internally; exported `AppendT/AppendTN/AppendNumber/AppendCurrency/AppendDate(dst []byte, ...)` for proven-hot paths (mirrors `strconv`). `AppendCurrency`'s floor is `money`'s internal decimal allocations, which are unreachable from this package; it is asserted to add zero allocations on top of that measured baseline.
- `bench_test.go` covers T (string key vs `Key`), TN, Currency, Number, middleware resolution, and Accept-Language parsing; post-benchmark optimization pass with before/after numbers in the PR (repo rule). `cldr` benchmarks rule evaluation.
- Codegen (typed message functions generated from catalogs, templ-style) is explicitly icebox — revisit only if benchmarks prove the runtime path insufficient.

## Errors & edge behavior

- Sentinels in `errors.go`: `ErrInvalidConfig`, `ErrInvalidCatalog`, `ErrDuplicateKey`, `ErrUnknownLocale`, `ErrUnknownKey`, `ErrNilRule` — all `errors.Is`-matchable, single-line.
- `Parse(tag) (Locale, error)`; `ParseOrDefault(tag) Locale` never fails (empty/unknown/malformed → default). Parsing is on `Bundle` (it knows the supported set and default), not package-level.
- Runtime never returns errors and never panics: missing key → key echo + miss handler; missing localizer in ctx → defaults; zero `Locale` → default; unconfigured plural rule → built-in default; unconfigured format → invariant. Rendering is total.

## Testing

- Black-box (`i18n_test` package), table-driven, testify (`require`/`assert`) per house style; testdata catalogs under `testdata/`. White-box `*_internal_test.go` only for unexported units.
- Fuzz: Accept-Language parser, placeholder compiler (`{{`-heavy adversarial messages), `normalizeTag`/`Parse`.
- Catalogs in testdata declare deliberately non-curated locales (e.g. `vi`) to prove the package supports languages it knows nothing about.
- `cldr` plural rules are differential-tested against x/text's CLDR tables (test-only dep) with a documented stale-data exception list.
- Unit tier only — no Docker, no integration tag.

## Anti-scope

No predefined locale list, plural rules, or format data in `core/i18n` — ever; that is the point of the design. No relative-time ladder. No runtime reload or DB-backed translations (bundles are immutable; rebuild to change). No ICU MessageFormat, gender, or ordinal rules (icebox). No bidi/RTL handling, collation, transliteration, or non-Gregorian calendars. No YAML. No translation-management tooling. No cookie writing in middleware. No Indian/variable digit grouping in v1.

## Catalog update (docs/packages.md)

`core/i18n` and `core/i18n/cldr` are **unbuilt**, so per `docs/design.md` ("the roadmap lists only unbuilt packages … if a package is listed, it does not exist yet") they belong in the roadmap until they ship. Replace the `## i18n/` section (four entries: `i18n/catalog`, `i18n/locale`, `i18n/numbers`, `i18n/dates`) with a `## core/` section holding `core/i18n` and `core/i18n/cldr`, and update `view/form`'s dep note from `i18n/catalog (planned)` to `core/i18n`. The implementation plan's final task **deletes** that `## core/` section when the package ships — a shipped package's godoc is its reference, and the catalog carries no shipped markers.

## Revision history

**Revision 2 (2026-07-17)** — replaced the curated-locale-table architecture with the mechanism/policy split, after revision 1 failed during implementation (Task 3 of 17). Revision 1 defined a static, framework-owned table of ≈30–40 (narrowed in planning to 16) curated locales; `Locale` was an index into it and the table gated which languages an application could use. Three defects surfaced, in escalating order of seriousness:

1. The gate's base-language fallback could not reach a locale curated *with* a region: `en-AU`→`en` worked, but `pt`, `pt-PT`, `zh`, `zh-TW`, and `zh-Hans-CN` all failed to resolve, because the only Portuguese and Chinese rows were `pt-BR` and `zh-CN` and the fallback map was keyed by tag. Verified by probe.
2. `ruleItalianPortuguese` bucketed two languages whose CLDR rules differ at `n = 0` (`pt` is `one: i = 0..1`; `it` is not), so `pt-BR` rendered "0 dias". Verified against x/text's CLDR tables. This is the same class of bug as the historical Slavic bucket, reintroduced by a table that had to be small enough to hand-curate.
3. The fundamental one: a framework-owned allowlist means the framework decides which languages an application may support. Shipping a `vi` catalog required a change to `core/i18n`. The table was also the only home for relative-time vocabulary, which put *translated text* in Go source beside application translations that live in JSON.

Defects 1 and 2 are symptoms; defect 3 is the cause. Revision 2 deletes the table, sources the locale set from the application's catalogs, demotes the CLDR data to an opt-in sibling package, and removes relative time (which the plural mechanism already expresses). Revision 1's plural rules and format data are not wasted — they are the seed of `core/i18n/cldr`.
