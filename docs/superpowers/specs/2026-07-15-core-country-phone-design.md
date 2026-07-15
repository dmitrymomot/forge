# Design: `core/country` + `core/phone`

Two roadmap packages built together (`country` first, `phone` depends on it), both modeled on the `core/money` precedent: curated static data exposed as package-level vars plus free-func lookups and small value types. Zero magic, no runtime data fetches.

## Scope & boundaries

- **`core/country`** — curated ISO-3166-1 static data (alpha-2/alpha-3/numeric, English name, primary currency, dial code, flag emoji), lookups, and a `Set` value type expressing a "supported countries" policy. Zero forge deps (stdlib only). Consumers: registration/KYC forms, `web/geoip` enrichment, `i18n`, `core/phone`.
- **`core/phone`** — E.164 normalization/decomposition value type plus a configured `Parser` (default region + supported-countries gate). Deps: `core/country`. Consumers: `comms/sms`, `auth/otp`, KYC forms.

**Not in scope (the libphonenumber swamp, stated explicitly):** per-country national-number length/pattern tables; line-type (mobile vs landline) or carrier detection; pretty per-country grouping (`(415) 555-2671`); disambiguating US-vs-CA (or any shared dial code) from a bare number via area-code ranges; ISO-3166-2 subdivisions/states; translated country names (that is `i18n`); historical/deprecated ISO codes; multi-currency-per-country reality (primary official currency only).

**Relationship to `core/validate`:** unchanged and complementary. `validate.CountryCode` / `validate.Phone` remain cheap yes/no *shape* rules returning a `Violation` with an i18n key (they may delegate to `phone.Parse` or use a standalone `^\+[1-9]\d{6,14}$` regex). `country`/`phone` are the authoritative *data + normalization* layer that returns values, which `validate` never does. Neither package changes `validate`.

## `core/country`

### Type & data

```go
type Country struct {
    Alpha2   string // "US"  — ISO-3166-1 alpha-2, the canonical key
    Alpha3   string // "USA" — ISO-3166-1 alpha-3
    Numeric  string // "840" — ISO-3166-1 numeric-3 (zero-padded)
    Name     string // "United States" — English short name
    Currency string // "USD" — primary official ISO-4217 code (string; no money import)
    DialCode string // "1"   — E.164 country calling code, no leading +
    Emoji    string // "🇺🇸"  — flag, derived from the alpha-2 regional-indicator pair
}
```

- All ~249 countries exposed as exported vars (`country.US` … `country.ZW`) plus a full bundled table backing the lookups. (Two-letter alpha-2 identifiers never collide with Go keywords.)
- `Currency` is the single primary official currency; countries with de-facto multiple currencies record only the primary, documented.
- `DialCode` is the country's single calling code. Many countries share one code (`+1`), so the reverse lookup returns a slice.
- `Emoji` is derived from the alpha-2 letters at data-generation time (two regional-indicator codepoints); it costs no runtime work and serves the flag-in-dropdown case.

### Lookups

All case-insensitive on input; unknown keys return the zero `Country` and `false`.

```go
func ByAlpha2(code string) (Country, bool)
func ByAlpha3(code string) (Country, bool)
func ByNumeric(code string) (Country, bool)
func ByDialCode(code string) []Country // may be many (US, CA, JM… for "1"); nil if none
func All() []Country                    // sorted by Name, deterministic; fresh slice per call
```

### `Set` — supported-countries policy

An explicit, immutable value the consumer constructs and passes. It is the single type serving both the filtered UI dropdown and the `phone` gate. May be backed by `core/set` internally, but exposes country-aware ergonomics.

```go
func NewSet(cs ...Country) Set
func NewSetFromCodes(codes ...string) (Set, error) // alpha-2 strings from config/env; unknown code → error (fail-closed)

func (s Set) Contains(c Country) bool
func (s Set) ContainsCode(code string) bool // case-insensitive
func (s Set) All() []Country                // sorted by Name — the filtered dropdown source
func (s Set) Len() int
```

### Files

`doc.go` (runnable example) · `country.go` (type + lookups) · `data.go` (generated exported vars + bundled table, with a source-provenance header comment) · `set.go` · `errors.go` · tests · `bench_test.go`.

## `core/phone`

Value type (`money.Money` precedent) plus a configured `Parser` (the sanctioned `New(...Option)` idiom, justified because phone genuinely carries config: a default region and an optional supported-countries gate). Deps: `core/country`.

### Value type

```go
type Phone struct { /* canonical E.164 digits + resolved country; pointer-free for GC */ }

func (p Phone) E164() string           // "+14155552671" — canonical; what sms/otp store & send
func (p Phone) DialCode() string       // "1"
func (p Phone) NationalNumber() string // "4155552671" — significant number after the dial code
func (p Phone) Country() (country.Country, bool) // primary country; bool=false when the dial code is ambiguous and no region hint resolved it
func (p Phone) Candidates() []country.Country     // all countries sharing the dial code — the correctness escape hatch
func (p Phone) IsZero() bool
```

**No pretty per-country grouping** — only the canonical E.164 form and the dial-code / national-number split. Localized grouping is the swamp and stays out.

### Marshaling

Mirrors `money`: SQL `Scan`/`Value` and JSON `MarshalJSON`/`UnmarshalJSON` all round-trip the E.164 string (`"+14155552671"`). The zero `Phone` marshals to empty/NULL; unmarshaling re-parses (fail on garbage).

### Free functions (zero-config)

```go
func Parse(input string) (Phone, error)              // requires a country code: leading + or 00
func ParseRegion(input, alpha2 string) (Phone, error) // bare national input; region supplies the dial code
```

`ParseRegion` strips a single leading trunk `0` (the near-universal national trunk-prefix convention) before prepending the region's dial code. Known limitation, documented: NANP uses no trunk `0`, and a few plans (e.g. Italian landlines) retain a significant leading `0` — callers in those regions pass fully-qualified `+` input to `Parse`.

### Configured parser

```go
func New(opts ...Option) *Parser
func WithDefaultRegion(alpha2 string) Option    // bare "415-555-2671" → +1…
func WithAllowedCountries(s country.Set) Option  // the supported-countries gate

func (p *Parser) Parse(input string) (Phone, error) // applies default region + gate
```

The gate rejects with `ErrUnsupportedRegion` only when the country is *provably* unsupported — the dial code maps to a single country, or a region hint resolved it. An ambiguous number (e.g. `+1`) with at least one supported candidate passes. Documented explicitly, because silent over-rejection would be worse than the honest gap.

### Parse rules

Strip formatting characters; normalize a leading `00` to `+`; match the longest dial prefix against `country`'s table; enforce E.164 bounds (≤15 total national digits, non-empty significant number). No per-country length or pattern tables. Failure modes below.

### Errors

`errors.Is`-matchable, single-line sentinels (`phone: …`):

- `ErrInvalidNumber` — non-digit garbage, or length outside E.164 bounds.
- `ErrMissingCountryCode` — `Parse` input has no `+`/`00` and no region context.
- `ErrUnknownDialCode` — leading digits match no country calling code.
- `ErrUnsupportedRegion` — resolved country is provably outside the configured `Set`.

### Files

`doc.go` (runnable example) · `phone.go` (type + free funcs + methods) · `parser.go` (`New` + gated `Parse`) · `config.go` · `options.go` (`type Option func(*config)`, never builders) · `sql.go` · `json.go` · `errors.go` · tests · `bench_test.go`.

## Cross-cutting

- **Tenancy.** No construction seam required: the supported-countries policy is a passed *value*. Single-tenant apps build one `Set`/`Parser`; multi-tenant apps resolve a per-tenant `Set` (or hold `map[TenantID]*Parser`). Zero ceremony single-tenant; fail-closed when a configured set is empty/missing.
- **Performance.** Map-backed O(1) lookups built once at package init; zero-alloc targets for `Parse` and the lookups (`useragent`/`money` precedent — scan bytes, avoid `[]byte`↔`string` round-trips); pointer-free `Phone`/`Country` structs for low GC scan cost (field layout enforced by betteralign). Each PR ships `bench_test.go` plus a measured post-benchmark optimization pass with before/after numbers.
- **Testing.** Black-box (`_test` external package) only; white-box only if unexported invariants demand it. Cover: every lookup hit/miss and case-folding; `Set` construction incl. fail-closed unknown code; `Parse`/`ParseRegion` happy paths, each error sentinel, `00`-prefix and trunk-`0` handling, shared-dial-code ambiguity (`Country` bool + `Candidates`); the gate's provably-unsupported vs ambiguous-passes behavior; SQL/JSON round-trips incl. zero value.
- **Data provenance.** `data.go` is curated/generated static ISO-3166 + dial + primary-currency data committed to the repo (no runtime fetch), with the source and generation date in a header comment.
- **Build order.** `country` (zero deps) ships first; `phone` builds on it.
- **Roadmap.** On ship, delete the `core/country` and `core/phone` entries from `docs/packages.md` (the catalog lists only unbuilt packages).
