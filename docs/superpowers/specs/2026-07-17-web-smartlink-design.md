# web/smartlink — Design

Destination-decision engine (the TDS/smartlink core) per the catalog entry: ordered rules of typed matchers evaluated over a caller-built visit context, first match wins, mandatory default target. Pure computation — no net/http, no storage, no HTTP handler. Rule storage/admin, target health checks, and bot filtering stay consumer-side. Deps: `core/clock` only.

## Shape

Two-phase API: `Compile` validates consumer-hydrated rule data fail-fast and precomputes everything; `Decide` is the infallible per-click hot path.

```go
func Compile(spec Spec, opts ...Option) (*Link, error)   // WithClock(clock.Clock), default clock.System()
func (l *Link) Decide(v Visit) Decision
```

## Types

```go
type Spec struct {
    Rules   []Rule      // ordered, first match wins; may be empty (default-only link)
    Default []Target    // mandatory, >= 1; weighted split when > 1
    Params  ParamPolicy // merge policy for Visit.Params into the final URL
}

type Rule struct {
    Name    string    // required, unique within the Spec (analytics identity + bucketing salt)
    When    []Matcher // AND semantics; empty = unconditional (maintenance-redirect override)
    Targets []Target  // >= 1; weighted split when > 1
}

type Target struct {
    URL    string // template; may contain {macros}
    Weight int    // relative weight; must be >= 1 when the target list has > 1 entry
}

type Visit struct {
    Country   string            // ISO 3166-1 alpha-2, any case
    Device    string            // web/useragent DeviceType vocabulary ("mobile", "desktop", ...)
    Locale    string            // BCP-47-ish: "en" or "en-US", any case
    StickyKey string            // bucketing identity (click ID / visitor ID); empty allowed
    At        time.Time         // optional decision-time override (click-log replay); zero = clock.Now()
    Params    map[string]string // inbound query params / sub-IDs
}

type Decision struct {
    Rule   string // matched rule name; "" = default target was used
    Target Target // the chosen raw (template-form) target
    URL    string // final rendered URL — what the caller redirects to and emits as the click event
}
```

## Matchers — closed typed vocabulary

`Matcher` is a sealed interface (unexported method); exactly six implementations, no consumer extension, no DSL:

- `Geo{Countries []string}` — any-of; compile validates 2 ASCII letters, normalizes uppercase; visit compares case-insensitively.
- `Device{Devices []string}` — any-of, case-insensitive; vocabulary aligned with `useragent.DeviceType` strings but not validated against it (consumer data).
- `Locale{Locales []string}` — any-of, case-insensitive. A bare-language rule value ("en") matches the visit's primary subtag ("en-US"); a region-qualified value ("en-US") requires a full match.
- `ParamEquals{Key string, Values []string}` — exact, case-sensitive any-of against `Visit.Params[Key]`.
- `TimeWindow{From, Until time.Time}` — matches `From <= t < Until` as absolute instants; either bound may be zero (open-ended), both zero is a compile error, `Until <= From` (both set) is a compile error. `t` is `Visit.At` when non-zero, else the link's clock.
- `Percent{Share int}` — deterministic traffic share, 1..99 (0 and 100 are compile errors: dead rule / drop the matcher). Matches when `fnv1a("p\x00" + ruleName, stickyKey) % 100 < Share`. Empty StickyKey never matches (fails closed to later rules / default — supplying the key is the caller's contract).

## Bucketing — deterministic, never RNG

FNV-1a 64 (featureflag precedent, inlined, allocation-free). Two independent salts so a rule's Percent gate and its split don't correlate: percent uses `"p\x00" + name`, weighted split uses `"s\x00" + name` (default targets: name = ""). Split: `h % totalWeight` walked against cumulative weights. Empty StickyKey deterministically takes the first target of a split.

## URL templates and macros

Vocabulary: `{country}`, `{device}`, `{locale}` (visit values, rendered as provided), and `{param.NAME}` (from `Visit.Params`). Parsed at compile; an unknown macro or malformed template is a compile error (postback precedent: never an empty substitution surprise at fire time — but a *known* macro whose visit value is empty renders as empty string, since visit context is genuinely sparse). Escaping is positional, decided at compile by splitting the template at the first `?`: path-segment macros render with `url.PathEscape`, query macros with `url.QueryEscape` — so macro values cannot alter URL structure and a compiled template always renders a parseable URL, keeping `Decide` error-free. Compile also validates that the template with macro values elided parses via `url.Parse`.

## Param merge policy

Applied after macro render, per link:

- `ParamsDrop` (zero value, default) — visit params never touch the URL; no parse, fastest path.
- `ParamsFill` — visit params are added only for keys the target URL doesn't already set.
- `ParamsOverride` — visit params replace the target's same-key values.

Fill/Override re-parse the rendered URL and re-encode the query (sorted, `url.Values.Encode`).

## Errors

`errors.Is`-matchable single-line sentinels, wrapped with rule/target context: `ErrNoDefault`, `ErrInvalidRule` (empty/duplicate name, no targets), `ErrInvalidTarget` (empty URL, bad weight), `ErrInvalidMatcher` (empty value list, bad percent, bad window, bad country code), `ErrInvalidTemplate`, `ErrUnknownMacro`. Compile-time only; `Decide` cannot fail.

## Tenancy

Pure engine over caller-supplied data: multi-tenant apps hydrate per-tenant rule sets and compile per-tenant Links (tenancy = passed value, `core/phone` precedent). No scope seam — there is no stored or keyed state to scope.

## Files

`doc.go` (runnable example) · `visit.go` · `rule.go` (Spec/Rule/Target/ParamPolicy) · `matcher.go` · `template.go` · `compile.go` · `decide.go` (+ bucketing) · `options.go` · `errors.go` · black-box tests + `bench_test.go` (Decide is the hot path: target ≤ 2 allocs for a no-merge macro-free decide; render/merge benched separately).

## Non-goals

Rule storage/admin UI · target health checks · bot filtering (compose `web/fingerprint`/`useragent` upstream) · HTTP redirect handler (consumer wires `Decide` output; `web/shortlink` owns code→handle resolution) · region/city geo targeting (countries only in v1) · recurring/dayparting time windows (absolute From/Until only in v1) · macro vocabulary extension.
