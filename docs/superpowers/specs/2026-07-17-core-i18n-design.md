# core/i18n — single-package internationalization

Date: 2026-07-17. Status: approved design, pre-plan.

## Decision

Build one package, `core/i18n`, replacing the four planned packages `i18n/catalog`, `i18n/locale`, `i18n/numbers`, `i18n/dates`. The `i18n/` domain section in `docs/packages.md` is replaced by a single `core/i18n` entry (and `view/form`'s dep note updates to `core/i18n`). Rationale: in real apps (SaaS, iGaming) translation, locale negotiation, and number/currency/date formatting are always used together; splitting them spread one feature across four imports, forced plural rules and a shared `Locale` type into an awkward home, and duplicated CLDR data between `numbers` and `dates`. The single package welds them deliberately and provides one-line ctx-based calls everywhere (handlers, templ, background jobs).

Heritage: this is the third iteration of the author's i18n design (boilerplate `pkg/i18n` → old-forge `pkg/i18n` → this). It keeps the proven ideas (immutable-after-New, flattened dot-notation catalogs, plural sub-keys with form fallback, language fallback chain, Translator/Localizer pattern, hardened Accept-Language parsing) and fixes the known flaws (per-call composite-key allocation, family-bucket plural bugs, float64 money, locale/format mismatch footgun, YAML dep).

## Package shape

- Path `core/i18n`, package `i18n`. Deps: `core/money`, `core/ctxkey`. No x/text, no YAML, no logger dep, no external deps.
- Idiom: `New(opts ...Option) (*Bundle, error)`; env-loadable `Config` (`I18N_DEFAULT_LOCALE`, `I18N_COOKIE_NAME`, `I18N_QUERY_PARAM`) + `DefaultConfig()` + `Validate()`, applied via `WithConfig`.
- Anatomy: `doc.go` (runnable example), `config.go`, `options.go`, `errors.go`, impl files, curated data tables in dedicated files (`plural_data.go`, `format_data.go`), `bench_test.go`.
- LOC guideline note: hand-written logic stays within the 250–850 band; curated data tables are excluded from the count (precedent: `core/country`'s 249 vars).
- Tenancy: locale is a passed value, not a scope — no `WithScope` seam (precedent: `core/country`). Per-tenant translation overrides, if ever needed, are separate `Bundle` instances constructed per tenant.

## Core types

- **`Locale`** — immutable value type: an index into the package's static curated locale table plus the original tag for display (`String()` returns e.g. `"uk-UA"`). Interned at parse time; all downstream lookups (catalog, plurals, format specs) are array indexing, never string hashing. The zero `Locale` is invalid and behaves as "unresolved" — every consumer falls back to the bundle default (fail-closed, never panics).
- **`Bundle`** — the immutable engine built by `New`: flattened message tables indexed `[localeIdx][keyIdx]`, precompiled message segments, plural rules, format specs, default locale, supported set. Safe for concurrent use; nothing mutates after `New`.
- **`Localizer`** — a two-word value `{*Bundle, Locale}` returned by `bundle.Ctx(ctx)` or `bundle.For(loc)`; the locale-bound view with the full method set. This is what travels in context and what `html/template` data embeds.
- **`Key`** — pre-resolved message-key handle for hot paths: `i18n.NewKey("dashboard.title")` at package var level; `New` validates all registered keys against the default locale's catalog (typo = construction error); lookup via a cached per-bundle index (atomic, falls back to map lookup when multiple bundles exist).
- **`PluralRule`** — `func(n int) PluralCategory` with `PluralCategory` an int enum (`Zero, One, Two, Few, Many, Other`). The custom-rule seam.

## Construction

```go
bundle, err := i18n.New(
    i18n.WithConfig(cfg),                       // or rely on DefaultConfig
    i18n.WithMessages(localesFS),               // fs.FS, JSON only, embed-friendly
    i18n.WithPlural("uk", customRule),          // override/add a plural rule
    i18n.WithFormatOverride("de", spec),        // tweak a curated format spec
    i18n.WithMissingHandler(onMiss),            // func(i18n.Miss), nil default = no-op
)
```

- **Loading**: `{lang}/{namespace}.json` under the fs.FS root; nested JSON flattened to dot notation; the namespace becomes the leading key segment (`email.invite.subject`) — no namespace parameter anywhere in the API. Duplicate keys across files = construction error.
- **Precompilation**: every message is parsed once at `New` into literal/placeholder segments (`{{name}}` syntax); rendering is a single builder pass with zero re-parsing. No in-flight file reads ever; catalogs are final at construction.
- **Supported set**: the union of languages present in the catalogs; the default locale must be among them. `Bundle.Locales()` exposes the set (drives negotiation and e.g. a language switcher UI).
- **Validation at `New`**: unparseable JSON, empty catalogs, unknown default, and registered `Key`s missing from the default catalog are construction errors. Plural-form probing (port of `SupportedPluralForms`) reports non-fatal findings — a message defining forms its language's rule never produces, or missing forms it does — through the miss handler with `Reason: MissingForm`.

## API surface — three layers

1. **Package-level ctx one-liners** (the 99% path; works in handlers, templ, jobs): `i18n.T(ctx, key, args...)`, `i18n.TN(ctx, key, n, args...)`, `i18n.TK/TNK(ctx, Key, ...)`, `i18n.Number(ctx, v)`, `i18n.Currency(ctx, money.Money)`, `i18n.Percent(ctx, v)`, `i18n.Date/Time/DateTime(ctx, t)`, `i18n.Relative(ctx, t)`, `i18n.LocaleFrom(ctx) Locale`. They read the `Localizer` stamped into ctx by the middleware or `Bundle.WithLocale`; if absent they fail closed: key echo for messages, invariant (en-US-like) formatting, never a panic.
2. **`Localizer`** (`bundle.Ctx(ctx)` / `bundle.For(loc)`): same method set minus the ctx parameter; embed in `html/template` render data (`{{ .L.T "dashboard.title" }}`), pass to batch rendering.
3. **Explicit `Bundle` methods** (`bundle.T(loc, key, ...)` etc.): no ctx anywhere; tests and multi-locale batch work.

Placeholder args are variadic key/value pairs: `i18n.T(ctx, "email.invite.subject", "name", inviter)` — one slice alloc, no map. `TN` auto-injects `count`.

Background jobs: `loc := bundle.ParseOrDefault(user.Locale)` then `ctx = bundle.WithLocale(ctx, loc)` — after that the same one-liners and templ components work identically to the HTTP path. Unknown recipient locale = empty string = default locale, a first-class path.

## Messages, plurals, fallback

- **Language fallback** per lookup: exact locale → base language (`en-US`→`en`) → default locale → key echo (+ miss handler with `Reason: MissingKey`).
- **Miss handler seam**: `WithMissingHandler(func(m Miss))` with `Miss{Locale, Key, Reason}` (`MissingKey` at runtime, `MissingForm` from load-time probing). Nil by default — a single nil check, zero overhead. More flexible than a logger dep: consumers log, notify, or record to DB. Documented contract: must be fast and non-blocking (it runs on the render path — offload heavy work to a channel/queue).
- **Plural sub-keys**: `"count": {"one": "...", "few": "...", "many": "...", "other": "..."}`. **Form fallback chain** when a translated form is missing: `two→few→many→other`, `few→many→other`, `zero→other`, `many→other`, `one→other`.
- **Per-language CLDR rules**, hand-curated per language (not family buckets — fixes the uk/ru "21 → one" class of bugs). `WithPlural(lang, rule)` overrides or adds.
- **Zero convention** (opt-in, per message): at `n == 0`, if the catalog defines a `zero` form it wins; otherwise the language's real CLDR rule decides. UX affordance for translators, never a grammar deviation.
- **validate bridge**: `core/i18n` does NOT import `core/validate`. `validate.Violation{Key, Params}` maps onto `T(loc, v.Key, pairs...)` with a trivial loop; `view/form` (which deps both) owns that adapter. `Localizer.T`'s signature is designed so the adapter is ~5 lines.

## Formatting

- **Curated per-locale `FormatSpec` table** (static data): decimal separator, grouping separator, 3-digit grouping (Indian grouping = icebox), percent placement, currency symbol placement + spacing, date/time/datetime layouts (Go layouts), relative-time patterns. Initial curated set ≈30–40 locales (en, en-GB, de, fr, es, pt, pt-BR, it, nl, pl, cs, sk, uk, ru, bg, sr, hr, ro, hu, el, tr, ar, he, ja, zh-CN, zh-TW, ko, th, vi, id, ms, sv, no, da, fi + extendable). Formats are selected by the same `Locale` value as messages — the old mismatch footgun (German text with US dates) is structurally impossible. `WithFormatOverride(tag, spec)` patches a bundle-local copy.
- **`Currency(loc, money.Money)`**: amount from `money.Decimal` rounded to the currency's `MinorUnits`; symbol from `money.Currency.Symbol`; the locale contributes separators and symbol placement only. Multi-currency correct: a de-DE user viewing USD sees `1.234,56 $`. No float64 money anywhere.
- **`Number`** takes float64 plus int64 variant (`NumberInt`); `Percent` takes a ratio (0.5 → `50 %` per locale).
- **`Relative(loc, t)`** ("3 hours ago", "через 2 години"): threshold ladder (now/seconds → minutes → hours → days → weeks → months → years), per-locale patterns using the same plural rules as messages. `RelativeAt(loc, t, now)` for testability.
- **Gregorian only.** Date/time layouts are per-locale presets, not user-composable pattern languages.

## HTTP integration

- `bundle.Middleware(resolvers...)` resolves the request locale via a resolver chain — defaults to cookie (`Config.CookieName`) → query (`Config.QueryParam`) → `Accept-Language` → default; explicit resolvers (`i18n.FromCookie(name)`, `i18n.FromQuery(name)`, `i18n.FromAcceptLanguage()`, `i18n.FromHeader(name)`) override the chain. It stamps the `Localizer` into ctx (single ctx key via `core/ctxkey`), sets `Content-Language`, and appends `Vary: Accept-Language` when the header participated.
- Accept-Language parser ports the old hardening: 4 KB header cap, q-value validation (0..1), quality-descending sort, server-preference tie-break on equal q (RFC 7231 §5.3.1), base-language partial matching, `*` resolves to the default locale.
- The middleware never writes cookies; persisting a user's choice is the consumer's concern (anti-scope).
- No logger integration exported: consumers wanting locale in log attrs wire a one-line `logger.ContextExtractor` over `i18n.LocaleFrom(ctx)` in their own logger config.

## Performance

- Interned `Locale` + key indices: `T` on the hot path is ctx read → two array indexes → plural func → segment builder. No string concat, no map hashing (string-keyed `T` keeps one map lookup; `Key` handles remove it).
- Precompiled segments render in one pass into a `strings.Builder` sized from the compiled length hint; target ≤2 allocs per `T` (result string + args slice), 0 extra for `Append` variants.
- Formatters are append-first internally; exported `AppendT/AppendTN/AppendNumber/AppendCurrency/AppendDate/AppendRelative(dst []byte, ...)` for proven-hot paths (mirrors `strconv`).
- `bench_test.go` covers T (string key vs `Key`), TN, Currency, Number, Relative, middleware resolution, and Accept-Language parsing; post-benchmark optimization pass with before/after numbers in the PR (repo rule).
- Codegen (typed message functions generated from catalogs, templ-style) is explicitly icebox — revisit only if benchmarks prove the runtime path insufficient.

## Errors & edge behavior

- Sentinels in `errors.go`: `ErrInvalidCatalog`, `ErrDuplicateKey`, `ErrUnknownLocale`, `ErrUnknownKey` (Key validation), `ErrInvalidConfig` — all `errors.Is`-matchable, single-line.
- `Parse(tag) (Locale, error)`; `ParseOrDefault(tag) Locale` never fails (empty/unknown/malformed → default). Parsing is on `Bundle` (it knows the supported set and default), not package-level.
- Runtime never returns errors: missing key → key echo + miss handler; missing localizer in ctx → defaults; zero `Locale` → default. Rendering is total.

## Testing

- Black-box (`i18n_test` package), table-driven; testdata catalogs under `testdata/`.
- Fuzz: Accept-Language parser, placeholder compiler (`{{`-heavy adversarial messages), `Parse`.
- Plural-rule tables verified against CLDR expectations per curated language (including the 21/22/25 Slavic cases, Arabic few/many bands, `zh/ja/ko` other-only).
- Unit tier only — no Docker, no integration tag.

## Anti-scope

No runtime reload or DB-backed translations (bundles are immutable; rebuild to change). No ICU MessageFormat, gender, or ordinal rules (icebox). No bidi handling, collation, transliteration, or non-Gregorian calendars. No YAML. No translation-management tooling. No cookie writing in middleware. No Indian/variable digit grouping in v1.

## Catalog update (docs/packages.md)

Replace the `## i18n/` section (four entries) with a single entry under `## core/`: `core/i18n` — message catalogs (JSON fs.FS, plurals with per-language CLDR rules + custom-rule seam, `{{name}}` placeholders), locale negotiation middleware + ctx carrier, and locale-aware number/currency/date/relative formatting over `core/money`; interned locales, precompiled messages, ctx one-liner API, nil-default miss-handler hook. Deps: `core/money`. Update `view/form`'s dep note from `i18n/catalog` to `core/i18n`.
