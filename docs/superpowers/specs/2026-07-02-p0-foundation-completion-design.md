# Design: P0 foundation completion — `errorsx`, `sanitize`, `slug`, `structfields`, `validate`, `filetype`, `decimal`, `money` (8 packages)

## Overview

Final batch of P0 foundation packages. With P1 crypto complete and the prior
leaf batches shipped (`slicex`/`ptr`/`mapx`/`set`/`encoding`/`enum`/`stringsx`,
then `typeconv`/`iox`/`bufpool`/`null`/`bytesize`), these eight close every
remaining gap in the P0 layer of `docs/maximal-package-set.md`.

| Package | Tier | Purpose |
|---|---|---|
| `validate` | **core** | Reflection-free, composable value/field validation emitting **i18n keys** (not English). The no-magic alternative to go-playground/validator. |
| `sanitize` | recommended | Plain-text normalization/escaping at trust boundaries (trim/collapse/strip-control, email/username canonicalization, filename + header-value + HTML escaping). |
| `slug` | recommended | URL-safe slug generation with Unicode→ASCII folding, an options API (length/separator/case/strip/replace/suffix/reserved), and predicate-based uniqueness. |
| `structfields` | recommended | The **one sanctioned reflection helper**: walk an exported struct's fields once (name/parsed-tag/value/Set), confining all `reflect` use to one audited primitive. |
| `filetype` | recommended | Detect a file's real MIME type from magic-byte signatures (not the client-supplied extension), closing the renamed-`.exe`-as-`.png` upload hole. |
| `money` | recommended | Money value with currency + exact arithmetic over `decimal`, penny-perfect `Allocate`. |
| `decimal` | optional | Fixed-point base-10 decimal for exact monetary/percentage math without binary-float drift. The numeric substrate `money` needs. |
| `errorsx` | optional | Lightweight coded-error + retryable/permanent tagging, for mapping internal errors to HTTP/API codes. No stack capture. |

`validate` is the sole **core** package and the highest-leverage item; `decimal`
carries the most implementation risk (exact decimal arithmetic) and gets a
correspondingly heavy, oracle-backed test suite.

## Design DNA (applies to every package)

- **Minimal deps, not zero.** Seven packages are stdlib-only. The single non-stdlib
  dependency is `golang.org/x/text/unicode/norm` in `slug` (Go-team quasi-stdlib,
  already in the module graph as indirect — promoted to direct). No external
  `validator`, `decimal`, `money`, or `filetype` library is taken.
- **Naming — the `x` suffix is a stdlib-collision escape, not decoration.** A package
  keeps an `x` suffix only when its natural name would collide with a stdlib package
  or a Go keyword: `errorsx` (stdlib `errors`), like the already-shipped `iox`/`hashx`/
  `randx`/`stringsx`/`subtlex`/`slicex`/`mapx`. A package with no such collision drops
  the `x`: hence **`slug`** (no stdlib `slug`) — and the previously-shipped `nullx` is
  **renamed to `null`** as part of this work (it has no stdlib counterpart; consistent
  with `set.Set`, which this repo already accepts). `sanitize`/`structfields`/`validate`/
  `filetype`/`decimal`/`money` are collision-free and take no suffix.
- **Idiom:** stateless free-funcs and generic value types; `validate` adds a small
  per-request collector (`*Validator`). **No builders** (CLAUDE.md). `validate`'s
  `Field(...).Field(...)` chaining is collector aggregation, not object construction.
- **Anatomy:** `doc.go` (package doc; scope + explicit "what this is NOT" + sibling
  pointers; a runnable `Example` is optional per P0-leaf precedent), `errors.go`
  (`errors.Is`-matchable single-line `pkg: …` sentinels, wrapped with `%w`), plus
  impl file(s) split by concern. Flat, single-responsibility files.
- **Go 1.26.** Use the `new(expr)` builtin over a `ptr.To` wrapper; run `just`
  `fmt`/`lint`/`test` (incl. `modernize`, `nilaway`, `betteralign`) before done.
- **Public methods never return unexported types.**
- **Black-box tests only** (`package X_test`), table-driven, with known-answer
  vectors where a standard/oracle exists.

## Build order (dependency waves)

```
Wave 1 (pure leaves, parallelizable):  errorsx · sanitize · structfields · decimal · filetype
Wave 2 (depend on Wave 1):             slug (norm + randx) · money (←decimal) · validate
```

`slug` folds via `x/text/norm` and draws random suffixes from the shipped `randx`;
it does its own normalization and does **not** import `sanitize`. `money` imports
`decimal`. `validate` is standalone but sequenced last (largest surface; benefits
from settled conventions).

---

## 1. `errorsx` — coded errors + retryable/permanent tagging (optional)

```go
// New creates a coded error. Errorf is the formatting variant.
func New(code, message string) error
func Errorf(code, format string, args ...any) error

// WithCode attaches a code to an existing error (wraps it, preserving the chain).
// WithCode(nil, _) returns nil.
func WithCode(err error, code string) error

// Code walks the chain and returns the nearest code and true, else ("", false).
func Code(err error) (string, bool)

// MarkPermanent tags err as non-retryable (wraps). IsPermanent reports the mark
// anywhere in the chain; IsRetryable is its negation (unmarked ⇒ retryable).
func MarkPermanent(err error) error
func IsPermanent(err error) bool
func IsRetryable(err error) bool
```

**Decisions**

- Coded errors are a small unexported type implementing `error`, `Unwrap` (when
  wrapping), and a `Code() string` method; `Code(err)` finds it via `errors.As`.
  `Error()` renders single-line: `"code: message"` when both present, else the
  message. **No stack capture** (honors the single-line-error rule).
- The permanent mark is a wrapper type with an unexported marker method;
  `IsPermanent` finds it via `errors.As`. An unmarked error is retryable by
  default, so `IsRetryable(err) == !IsPermanent(err)`. This complements the future
  `retry` package's own `Permanent()` stop sentinel (which may adopt this later).
- `errors.Is`/`As` pass through `Unwrap`; `Code`/`MarkPermanent` compose with any
  wrapped sentinel.
- **Out of scope:** stack traces, HTTP-status mapping (that lives in the future
  `problem`/render layer, which reads `Code`).

Deps: `errors`, `fmt`.

---

## 2. `sanitize` — plain-text normalization at trust boundaries (recommended)

For **untrusted** input. NOT a rich-HTML sanitizer (no bluemonday); `HTML` only
escapes. Trusted developer-facing string shaping lives in `stringsx` — `sanitize`
deliberately does **not** duplicate `Truncate`.

```go
func Trim(s string) string          // strings.TrimSpace
func Collapse(s string) string      // trim + collapse internal whitespace runs → single space
func StripControl(s string) string  // drop Unicode Cc/Cf control chars, keep printable + normal spaces
func SingleLine(s string) string    // remove line breaks (CR/LF/Unicode) then Collapse
func Email(s string) string         // canonicalize: trim + lowercase (NOT validation — see validate.Email)
func Username(s string) string      // trim + lowercase + keep [a-z0-9._-], trim leading/trailing separators
func HTML(s string) string          // html.EscapeString
func Filename(s string) string      // base name; strip path separators, control, leading dots ("" if nothing safe remains)
func HeaderValue(s string) string   // strip CR/LF + control to prevent header/response-splitting injection
```

**Decisions**

- `Email` **canonicalizes, it does not validate** — validation (structure, MX) is
  `validate.Email`. Canonicalization is trim + `ToLower` of the whole address.
- `Filename` strips both `/` and `\` (OS-independent, via `strings`, not
  `filepath`), removes control chars and leading dots (blocks `.`/`..`/hidden), and
  returns `""` when nothing safe remains (caller decides the fallback name).
- `HeaderValue` removes `\r`, `\n`, and other control runes — the CRLF-injection
  guard for any value flowing into a response header.
- `StripControl` keeps regular spaces; `SingleLine` is the line-break-specific
  variant used for single-line form fields.

Deps: `strings`, `unicode`, `unicode/utf8`, `html`.

---

## 3. `slug` — URL-safe slugs with options (recommended)

Ports the prior `pkg/slug` options API (the reference implementation at commit
`0b68f05`) onto forge conventions: NFKD folding via `x/text`, and random suffixes
drawn from the shipped `randx` (not a private `crypto/rand`).

```go
type Option func(*config)   // config is unexported; options are the only knob (no builder)

// Make folds arbitrary text to a URL-safe slug (default: lowercase, "-" separated,
// [a-z0-9] with single "-" between word runs). Returns "" for input with no
// sluggable characters (unless a suffix/min-length option forces a random slug).
func Make(s string, opts ...Option) string

// Unique returns Make(s, opts…), or the first of "<slug>-2", "<slug>-3", … for which
// exists(candidate) reports false. Human-friendly incrementing suffix, storage-
// agnostic via the predicate — distinct from the RANDOM suffix of WithSuffix /
// WithReservedSlugs.
func Unique(s string, exists func(candidate string) bool, opts ...Option) string

// Options (ported from the reference impl)
func WithSeparator(sep string) Option                  // default "-"
func WithLowercase(enabled bool) Option                // default true
func WithMaxLength(n int) Option                       // truncate to n runes (0 = unlimited)
func WithMinLength(n int) Option                       // pad with a random suffix up to n runes
func WithStripChars(chars string) Option               // delete these runes before folding
func WithCustomReplace(repl map[string]string) Option  // {"&":"and","@":"at"} applied FIRST
func WithSuffix(length int) Option                     // append a random [a-z0-9] suffix of length
func WithReservedSlugs(slugs ...string) Option         // if result is reserved (case-insensitive), append a random suffix
```

**Decisions**

- **Pipeline order (documented, from the reference impl):** `WithCustomReplace` →
  `WithStripChars` → per-rune fold → trim separator → `WithMaxLength` truncation →
  reserved/`WithSuffix`/`WithMinLength` suffixing. Per-rune fold: ASCII `[a-z0-9]`
  passes through; letters decompose via **NFKD** (`x/text/unicode/norm`) with
  combining marks dropped (`é`→`e`, `ü`→`u`); a small **special-case map** covers
  non-decomposing Latin runes NFKD leaves intact (`ß`→`ss`, `ø`→`o`, `ł`→`l`,
  `đ`→`d`, `æ`→`ae`, `œ`→`oe`); every other run collapses to a single separator.
- **NFKD + special-case map, not the old hand-rolled `diacriticMap`.** `norm` handles
  the whole `à/á/â/ä/ā/ă/ą…` space automatically (far broader than the reference
  table) while the tiny special-case map covers what NFKD cannot decompose. This
  keeps the approved `x/text/norm` decision and drops ~60 lines of hand-maintained data.
- **Random suffixes use `randx`.** `WithSuffix`, `WithMinLength`, and the reserved-hit
  path draw a bias-free `[a-z0-9]` string from `randx` (per-char `randx.Int`), not a
  local `crypto/rand` with modulo bias as in the reference impl. Uppercase is added to
  the alphabet only when `WithLowercase(false)`.
- **`Unique` vs random suffixes.** `Unique(s, exists, …)` appends a human-friendly
  incrementing `-2`/`-3`/… (blog-post style) until the predicate clears — a different
  collision strategy from the random `WithSuffix`/`WithReservedSlugs`. Both ship.
- `slug` does **not** import `sanitize` (its transform is distinct). **`MakeLang`
  dropped for v1** (YAGNI); addable later without an API break.
- Non-Latin scripts with no ASCII fold collapse to `""` (callers fall back to an id);
  `WithMinLength`/`WithSuffix` still yield a random-only slug from an empty base.

Deps: `golang.org/x/text/unicode/norm`, forge `randx`, `strings`, `unicode`, `unicode/utf8`.

---

## 4. `structfields` — the one sanctioned reflection helper (recommended)

Confines all `reflect` usage to a single audited primitive so `envconfig`/form/
scan stay reflection-free themselves. Pairs with `typeconv` for the scalar parse.

```go
type Tag struct {
    Name    string   // first comma-segment of the tag ("" when absent)
    Options []string // remaining comma-separated segments
    Raw     string   // raw tag value for tagKey
}
func (t Tag) Ignored() bool           // Name == "-"
func (t Tag) HasOption(opt string) bool

type Field struct {
    Name  string            // Go field name
    Tag   Tag               // parsed tagKey tag
    Value reflect.Value     // settable when the input was a non-nil *struct
    Set   func(v any) error // assign/convert into Value; ErrNotSettable / type error
}

// Walk visits each exported field of a struct (or non-nil *struct) once.
func Walk(v any, tagKey string, fn func(Field) error) error

var ErrNotStruct   = errors.New("structfields: not a struct")
var ErrNotSettable = errors.New("structfields: field not settable")
```

**Decisions**

- Accepts a struct **or** non-nil `*struct`. A pointer is required for `Set`/settable
  `Value`; a non-pointer still walks (read-only, `Set` returns `ErrNotSettable`).
  Anything else → `ErrNotStruct`.
- **Only exported fields** are visited. **Shallow** in v1: anonymous embedded
  structs are yielded as a single field, not flattened — a caller needing recursion
  re-`Walk`s that field. (Flattening + prefixing can be added later without an API
  break.)
- `Set` converts assignable or convertible values; type mismatch returns a wrapped
  error, a non-settable target returns `ErrNotSettable`.
- `reflect.Value`/`reflect.StructTag` are stdlib, so they are allowed as public types
  (the framework's single sanctioned reflection surface).

Deps: `reflect`, `strings`, `errors`, `fmt`.

---

## 5. `validate` — reflection-free validation, i18n-key output (core)

The deliberate no-magic alternative to struct-tag validators. A failed rule emits a
**stable i18n key** (e.g. `validation.required`) plus interpolation params — never a
baked-in English sentence. `validate` has **no i18n dependency**; it only produces
keys + params for a downstream i18n layer to render.

```go
// Violation is one failed rule. Every rule populates a stable i18n Key plus the
// structured Params that both the default i18n message and any custom Message
// template interpolate. Message, when set (via Msg), is the final rendered text.
type Violation struct {
    Key     string         `json:"key"`               // "validation.required", "validation.between", …
    Params  map[string]any `json:"params,omitempty"`  // {"min":18,"max":72}; nil when the rule has none
    Message string         `json:"message,omitempty"` // literal override, already interpolated; empty ⇒ render Key+Params downstream
}

// Rule evaluates a pre-bound value; returns nil when valid, else a *Violation.
type Rule func() *Violation

// Errors maps field name → violations. Implements error; marshals as
// {"email": [{"key":"validation.email"}]}.
type Errors map[string][]Violation
func (e Errors) Error() string          // single-line, sorted, for logs
func (e Errors) Has(field string) bool

// Validator collects field violations (per-request; not goroutine-safe).
type Validator struct { /* errs Errors */ }
func New() *Validator
func (v *Validator) Field(name string, rules ...Rule) *Validator // runs all rules, collects all failures
func (v *Validator) Add(field, key string, params map[string]any) // manual violation
func (v *Validator) Err() error         // nil when clean, else the Errors value

// Rules — each is pre-bound to its value and returns a Rule:
func Required[T comparable](value T) Rule            // validation.required (zero-value ⇒ fail)
func NotBlank(s string) Rule                         // validation.not_blank (whitespace-only ⇒ fail)
func MinLen(s string, n int) Rule                    // validation.min_len   {min}
func MaxLen(s string, n int) Rule                    // validation.max_len   {max}
func LenBetween(s string, min, max int) Rule         // validation.len_between {min,max}
func Email(s string) Rule                            // validation.email
func URL(s string) Rule                              // validation.url (http/https absolute)
func Match(s string, re *regexp.Regexp, key string) Rule // caller-named key
func OneOf[T comparable](value T, allowed ...T) Rule // validation.one_of  {allowed}
func Min[T cmp.Ordered](value, min T) Rule           // validation.min     {min}
func Max[T cmp.Ordered](value, max T) Rule           // validation.max     {max}
func Between[T cmp.Ordered](value, min, max T) Rule  // validation.between {min,max}
func True(ok bool, key string) Rule                  // escape hatch for a custom predicate

// Overrides — decorate ANY rule; take effect only when the wrapped rule fails.
// Msg sets a literal message, interpolating the failing rule's Params into
// {name} placeholders ("must be {min}-{max}" ⇒ "must be 18-72"); unknown
// placeholders are left verbatim. WithKey swaps the i18n Key, preserving Params.
func Msg(r Rule, template string) Rule
func WithKey(r Rule, key string) Rule
```

**Per-rule error structure (the Key + Params contract — stable public API):**

| Rule | `Key` | `Params` |
|---|---|---|
| `Required` / `NotBlank` | `validation.required` / `validation.not_blank` | — |
| `MinLen(n)` / `MaxLen(n)` | `validation.min_len` / `validation.max_len` | `{min}` / `{max}` |
| `LenBetween(min,max)` | `validation.len_between` | `{min, max}` |
| `Email` / `URL` | `validation.email` / `validation.url` | — |
| `OneOf(a…)` | `validation.one_of` | `{allowed}` (slice) |
| `Min(m)` / `Max(m)` | `validation.min` / `validation.max` | `{min}` / `{max}` |
| `Between(min,max)` | `validation.between` | `{min, max}` |
| `Match(re,key)` / `True(ok,key)` | caller's `key` | — |

So `validate.Msg(validate.Between(age, 18, 72), "Your age must be between {min} and {max}")`
yields `Violation{Key:"validation.between", Params:{min:18,max:72}, Message:"Your age must be between 18 and 72"}`.

**Decisions**

- **i18n keys are the public contract.** The `validation.<rule>` namespace and each
  rule's `Params` keys are documented and stable so i18n catalogs can target them.
  `Match`/`True` take a caller-supplied key (there is no default sentence to emit).
- **Overrides are decorator combinators**, chosen so they wrap *any* rule uniformly —
  built-ins, `Match`/`True`, and arbitrary `func() *Violation` closures — without
  growing 12 rule signatures. `Msg(rule, template)` interpolates the failing rule's
  `Params` into `{name}` placeholders and stores the result in `Violation.Message`
  (a literal, display-ready string that bypasses i18n). `WithKey(rule, key)` swaps the
  key while keeping `Params`. Both are no-ops when the wrapped rule passes.
  Interpolation is a dependency-free `{name}`→`fmt.Sprint(Params[name])` replace; a
  placeholder with no matching param is left verbatim (so typos stay visible).
- `Field` **collects all** failing rules for a field (does not stop at the first),
  appending each `*Violation` under the field name. It returns `*Validator` for
  `v.Field(...).Field(...)` chaining — aggregation, **not** a builder.
- `Required` uses zero-value comparison of a `comparable` (so `""`, `0`, zero-time
  are "missing"); whitespace-only strings pass `Required` but fail `NotBlank`.
- `Email` = `net/mail.ParseAddress` + a light structural check (no DNS). `URL` =
  `net/url.Parse` requiring an `http`/`https` scheme and a host.
- `Errors` marshals to JSON as `field → [{key, params?, message?}]` (API-friendly;
  `message` present only when set via `Msg`) and `Error()` renders a sorted single
  line (log-friendly). `Validator` is per-request, documented as not goroutine-safe.
- **Out of scope:** struct-tag binding (→ `structfields` + these rules); *key-based*
  message rendering (→ future `i18n`; `validate` only emits Key+Params) — note the
  *literal* `Msg` template is interpolated in-package, so it needs no i18n layer;
  cross-field rules beyond what `Add`/`True`/closures express.

Deps: `regexp`, `net/mail`, `net/url`, `strings`, `unicode/utf8`, `cmp`, `sort`, `fmt`.

---

## 6. `decimal` — exact fixed-point base-10 arithmetic (optional) — **highest rigor**

Exact decimal for monetary/percentage math; the substrate `money` needs (int-cents
alone silently mishandles `8.25%` tax). **This package's arithmetic is the batch's
top correctness risk and gets an oracle-backed, property-based, fuzzed test suite.**

```go
// Decimal is an exact base-10 fixed-point number: value = coef × 10^(−scale),
// scale ≥ 0. The zero value is 0. Fast path keeps coef in int64; any operation
// that would overflow transparently promotes to math/big and demotes back when
// the result again fits int64.
type Decimal struct { /* coef int64; big *big.Int (non-nil ⇒ big mode); scale int32 */ }

type RoundingMode int
const (
    HalfEven RoundingMode = iota // banker's rounding — DEFAULT
    HalfUp                       // halves away from zero
    HalfDown                     // halves toward zero
    Up                           // always away from zero
    Down                         // always toward zero (truncate)
    Ceil                         // toward +∞
    Floor                        // toward −∞
)

// Constructors
func New(coef int64, scale int32) Decimal
func FromInt(i int64) Decimal
func Parse(s string) (Decimal, error)      // ErrSyntax
func MustParse(s string) Decimal
var Zero Decimal

// Exact arithmetic (scale grows as needed; big.Int on overflow; no rounding)
func (d Decimal) Add(e Decimal) Decimal    // scale = max(scales)
func (d Decimal) Sub(e Decimal) Decimal    // scale = max(scales)
func (d Decimal) Mul(e Decimal) Decimal    // scale = d.scale + e.scale
func (d Decimal) Neg() Decimal
func (d Decimal) Abs() Decimal

// Division REQUIRES an explicit result scale + rounding mode (no non-terminating
// "exact" divide). e == 0 ⇒ ErrDivByZero.
func (d Decimal) Div(e Decimal, scale int32, mode RoundingMode) (Decimal, error)

// Comparison / predicates (scale-normalized: 2.50 Equal 2.5)
func (d Decimal) Cmp(e Decimal) int
func (d Decimal) Equal(e Decimal) bool
func (d Decimal) Sign() int
func (d Decimal) IsZero() bool

// Rounding / rescale
func (d Decimal) Round(places int32, mode RoundingMode) Decimal
func (d Decimal) Rescale(scale int32, mode RoundingMode) Decimal
func (d Decimal) Scale() int32

// Conversion
func (d Decimal) String() string                 // preserves stored scale: 2.50 → "2.50"
func (d Decimal) Float64() (f float64, exact bool)
func (d Decimal) MarshalText() ([]byte, error)
func (d *Decimal) UnmarshalText(p []byte) error

var ErrSyntax    = errors.New("decimal: invalid syntax")
var ErrDivByZero = errors.New("decimal: division by zero")
```

**Arithmetic semantics (the emphasis)**

- **Representation invariant:** `value = coef × 10^(−scale)`, `scale ≥ 0`. Exactly one
  of `int64 coef` / `*big.Int big` is active (`big != nil` ⇒ big mode). Every
  operation restores the invariant and demotes big→int64 when it fits.
- **Add/Sub:** align operands to `max(scale)` by multiplying the smaller coef by
  `10^Δ` (this widening can itself overflow ⇒ big path), then add/subtract. Result
  scale = `max(scales)`. **Exact — never rounds.**
- **Mul:** `coef = coefₐ·coef_b` (overflow ⇒ big), `scale = scaleₐ + scale_b`.
  **Exact — never rounds.**
- **Div:** computed as `(coefₐ · 10^(scale + scale_b − scaleₐ)) / coef_b` in `big.Int`,
  applying `mode` to the remainder, producing exactly `scale` fractional digits.
  Requiring the caller to name scale+mode makes non-terminating quotients (`1/3`)
  well-defined. `e == 0` ⇒ `ErrDivByZero`.
- **Round/Rescale:** `Round(places, mode) = Rescale(places, mode)`. Increasing scale
  is exact (pad zeros); decreasing applies `mode` to the discarded low digits.
- **Rounding modes** are implemented on the discarded remainder with correct sign
  handling (`Ceil`/`Floor` are sign-aware; `HalfUp` rounds halves away from zero,
  `HalfDown` toward zero, `HalfEven` to the even neighbor). `HalfEven` is the default.
- **Cmp/Equal** align scales (in big to avoid overflow) and compare numerically, so
  `2.50 Equal 2.5` is true even though `String()` preserves the stored scale.
- **String/Parse** round-trip preserves scale (`Parse(d.String())` reproduces
  `coef`+`scale`). `Parse` accepts optional sign, integer/fraction digits, one `.`;
  scientific notation is **out of scope for v1** (rejected as `ErrSyntax`).
- **Float64** is best-effort with an `exact` flag derived via `big.Rat` (documented
  lossy — for display/interop only, never for money math).

**Test strategy (heaviest in the batch)**

- **`math/big.Rat` oracle / property tests:** for randomized `a,b`, assert
  `a.Add(b)`, `a.Sub(b)`, `a.Mul(b)` equal the `big.Rat` computation exactly
  (compare via `Decimal.String` parsed back into a `Rat`); assert `Div(...,scale,mode)`
  equals the `Rat` quotient rounded to that scale/mode.
- **Round-trip:** `Parse(d.String()) == d` (coef & scale) for randomized `d`.
- **Algebraic laws:** `a+0==a`, `a−a` is numerically 0, `a*1==a`, associativity of
  `+`/`*` (numeric), `Neg(Neg(a))==a`.
- **Overflow boundary:** operations that cross `int64` min/max promote to big and
  demote back, matching the oracle exactly.
- **Rounding tables:** exhaustive per-mode vectors covering exact halves
  (`0.5, 1.5, 2.5, −0.5, −2.5`) and non-halves, positive and negative.
- **Errors:** `Div` by zero ⇒ `ErrDivByZero`; junk input ⇒ `ErrSyntax` (`errors.Is`).
- **`go test -fuzz`** targets for `Parse` and for `Add`/`Mul` against the `Rat` oracle.

Deps: `math/big`, `strings`, `strconv`, `errors`, `fmt`.

---

## 7. `money` — currency-aware money over `decimal` (recommended)

```go
type Currency struct {
    Code       string // ISO-4217 alpha, "USD"
    Num        string // ISO-4217 numeric, "840"
    MinorUnits int32  // fractional digits: USD 2, JPY 0, BHD 3
    Symbol     string // "$" — may equal Code when there is no distinct symbol
}

type Money struct { /* amount decimal.Decimal; currency Currency */ }

func New(amount decimal.Decimal, c Currency) Money
func FromMinor(units int64, c Currency) Money       // 150, USD ⇒ 1.50 USD
func Parse(s string, c Currency) (Money, error)

func (m Money) Amount() decimal.Decimal
func (m Money) Currency() Currency
func (m Money) Minor() int64                          // amount rounded to MinorUnits, as minor units

func (m Money) Add(n Money) (Money, error)            // ErrCurrencyMismatch
func (m Money) Sub(n Money) (Money, error)
func (m Money) Mul(factor decimal.Decimal) Money       // exact; caller Rounds for settlement
func (m Money) Round(mode decimal.RoundingMode) Money  // to MinorUnits
func (m Money) Allocate(ratios ...int) ([]Money, error) // penny-perfect; ErrInvalidAllocation
func (m Money) Split(n int) ([]Money, error)            // equal split, remainder spread over first parts

func (m Money) Cmp(n Money) (int, error)              // ErrCurrencyMismatch
func (m Money) IsZero() bool
func (m Money) String() string                        // "1.50 USD"

// ISO-4217 registry
var (USD, EUR, GBP, JPY, CHF, /* … full table … */ Currency)
func CurrencyByCode(code string) (Currency, bool)

var ErrCurrencyMismatch  = errors.New("money: currency mismatch")
var ErrUnknownCurrency   = errors.New("money: unknown currency")
var ErrInvalidAllocation = errors.New("money: invalid allocation ratios")
```

**Decisions**

- **Amount is a `decimal.Decimal`, not int-cents** — so percentage/tax math stays
  exact; `MinorUnits` governs rounding at settlement/display (`Minor`, `Round`,
  `String`). `Mul` (e.g. by a tax rate) keeps full precision; the caller `Round`s.
- `Add`/`Sub`/`Cmp` require identical `Currency.Code` ⇒ `ErrCurrencyMismatch`.
- `Allocate(ratios…)`: split proportionally to `ratios`, penny-perfect at
  `MinorUnits` via **largest-remainder** so `sum(parts) == m` exactly; `sum(ratios) ≤ 0`
  ⇒ `ErrInvalidAllocation`. `Split(n)` is equal division with the remainder minor
  units distributed to the first parts.
- **Full ISO-4217 minor-units table** ships as static data (`currency_data.go`);
  symbols filled for common currencies, else `Symbol == Code`. Complete-but-cheap
  beats a curated subset users outgrow.
- `String` renders `"1.50 USD"` (unambiguous, locale-free); locale/symbol
  formatting is deferred to the future i18n layer.
- **Out of scope:** FX/currency conversion, locale-aware formatting.

Deps: `decimal`, `strings`, `errors`, `fmt`.

---

## 8. `filetype` — magic-byte MIME detection (recommended)

Detects a file's **real** type from content signatures, not the client-supplied
extension/Content-Type — the upload-spoofing guard.

```go
type Type struct { MIME, Ext string }

// Detect matches head against the curated signature table; falls back to
// net/http.DetectContentType. ok is false only when even the fallback is
// application/octet-stream (i.e. genuinely unrecognized).
func Detect(head []byte) (Type, bool)

// DetectReader peeks up to a fixed head (≤512B) WITHOUT consuming r, returning a
// reader that replays the full stream (io.MultiReader of the peeked head + r).
func DetectReader(r io.Reader) (Type, io.Reader, error)

// Is reports whether head's detected type has the given MIME.
func Is(head []byte, mime string) bool
```

**Decisions**

- **Curated signature table** (authoritative for the security use-case): images
  (png, jpeg, gif, webp, bmp, tiff, ico), pdf, zip, gzip, tar, mp3 (ID3 + frame
  sync), wav, ogg, flac, mp4/mov (`ftyp`), webm (EBML). Pure data + `bytes` compares,
  no I/O.
- **OOXML caveat:** docx/xlsx/pptx share the `PK` zip signature, so `Detect` reports
  `application/zip`; distinguishing them requires reading the archive directory and
  is **out of scope** (documented). Magic-byte sniffing still closes the
  renamed-executable hole, which is the point.
- **Fallback to `net/http.DetectContentType`** on a table miss; `ok=false` only when
  the result is `application/octet-stream`.
- `DetectReader` uses `io.ReadFull` + `io.MultiReader` (stdlib) to stay
  re-readable for non-seekable streams — it does **not** need `iox` (a deliberate
  simplification vs. the roadmap sketch).

Deps: `bytes`, `io`, `net/http`, `strings`, `errors`.

---

## Cross-cutting concerns

### Dependencies

| Package | Non-stdlib | stdlib |
|---|---|---|
| `errorsx` | — | errors, fmt |
| `sanitize` | — | strings, unicode, unicode/utf8, html |
| `slug` | **golang.org/x/text/unicode/norm** | forge `randx`; strings, unicode, unicode/utf8 |
| `structfields` | — | reflect, strings, errors, fmt |
| `validate` | — | regexp, net/mail, net/url, strings, unicode/utf8, cmp, sort, fmt |
| `decimal` | — | math/big, strings, strconv, errors, fmt |
| `money` | forge `decimal` | strings, errors, fmt |
| `filetype` | — | bytes, io, net/http, strings, errors |

Forge edges inside the batch: `money → decimal`, `slug → randx`. Only external
addition: `x/text/norm` becomes a direct dep (already indirect). `go mod tidy` after `slug`.

### Build approach

TDD each package (tests first). Wave 1 packages are independent and can be built in
parallel; Wave 2 follows. Run `just check` (fmt + lint + test with `-race`) and the
full `just lint` (vet, build, golangci-lint, nilaway, betteralign, modernize) before
declaring any package done.

### Top risks

1. **`decimal` arithmetic correctness** — overflow promotion/demotion, rounding
   modes on negatives, exact-half behavior. Mitigation: the `big.Rat` oracle,
   property tests, exhaustive rounding tables, and fuzzing described above. This is
   the package to over-test.
2. **`money.Allocate` penny-perfection** — `sum(parts) == total` must hold for every
   ratio set and both directions of remainder; assert the invariant over randomized
   ratios/amounts.
3. **`slug` Unicode folding + option interactions** — verify NFKD + combining-mark
   stripping and the special-case map (ß/ø/ł/…) across Latin diacritics and
   full-collapse (CJK ⇒ `""`); and that `WithMaxLength` truncation composes correctly
   with a random/reserved suffix (total length never exceeds the cap).
4. **`structfields` settability** — confirm `Set` works only through a non-nil
   `*struct` and that a value-struct walk is read-only (`ErrNotSettable`).
5. **`filetype` re-readability** — `DetectReader` must replay the full stream for a
   non-seekable reader (assert bytes are not lost after detection).

### Out of scope (deliberate)

- `errorsx`: stack traces; HTTP-status mapping (→ `problem`).
- `sanitize`: rich-HTML sanitization (bluemonday); `Truncate` (→ `stringsx`).
- `slug`: per-language transliteration (`MakeLang`).
- `structfields`: embedded-struct flattening; struct-tag *binding* (→ consumers).
- `validate`: message rendering (→ `i18n`); struct-tag DSL.
- `decimal`: scientific-notation parsing; arbitrary-precision "exact" division.
- `money`: FX conversion; locale-aware formatting.
- `filetype`: OOXML sub-type disambiguation; deep container parsing.
