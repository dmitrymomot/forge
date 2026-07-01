# Design: P0 foundation completion — `errorsx`, `sanitize`, `slug`, `structfields`, `validate`, `filetype`, `decimal`, `money` (8 packages)

## Overview

Final batch of P0 foundation packages. With P1 crypto complete and the prior
leaf batches shipped (`slicex`/`ptr`/`mapx`/`set`/`encoding`/`enum`/`stringsx`,
then `typeconv`/`iox`/`bufpool`/`null`/`bytesize`), these eight close every
remaining gap in the P0 layer of `docs/maximal-package-set.md`.

| Package | Tier | Purpose |
|---|---|---|
| `validate` | **core** | Reflection-free, composable value/field validation emitting **i18n keys** (not English); rich rule set incl. string/format/network/numeric/collection + e-commerce (Luhn/EAN/ISBN/ISO codes) & crypto-address (BTC/ETH/TRON/SOL) groups. The no-magic alternative to go-playground/validator. |
| `sanitize` | recommended | Plain-text normalization/escaping at trust boundaries + a generic `Apply`/`Compose` pipeline: whitespace/control, char-class filters, HTML escape/strip, email/username/filename/header/URL canonicalizers. |
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
  or a Go keyword *and* has no better domain noun: `errorsx` (stdlib `errors`), like
  the shipped grab-bags `iox`/`stringsx`/`slicex`/`mapx` (whose honest names —
  `io`/`strings`/`slices`/`maps` — all collide, and whose only non-`x` alternative is
  a discouraged `*util` name). A collision-free package drops the `x` (**`slug`**), and
  a package with a real domain noun takes that noun instead of `x`. Applied alongside
  this work as separate refactors: `nullx`→`null`, `hashx`→`digest`, `randx`→`random`,
  `subtlex`→`consttime`. (`htmx` stays — it is the HTMX library, not `html`+x.)
  `sanitize`/`structfields`/`validate`/`filetype`/`decimal`/`money` are collision-free
  and take no suffix.
- **Idiom:** stateless free-funcs and generic value types throughout — **no instances,
  no builders** (CLAUDE.md). Even `validate` is free functions: a rule is
  `Rule[T] = func(T) Violation` (`sanitize`'s `func(T) T` analog), composed by
  `Apply`/`Check` and a small rule algebra.
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
Wave 2 (depend on Wave 1):             slug (norm + random) · money (←decimal) · validate
```

`slug` folds via `x/text/norm` and draws random suffixes from the shipped `random`;
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

For **untrusted** input. NOT a rich-HTML sanitizer (no bluemonday) — see the
explicit exclusions below. Trusted developer-facing string shaping (case
conversion, `Truncate`, `Mask`) lives in `stringsx`; `sanitize` does not duplicate it.

**Composition primitive** (ported from the reference `pkg/sanitizer` — the pattern
the user liked). Every one-arg sanitizer has signature `func(string) string`, so
they chain cleanly:

```go
// Apply runs transforms left-to-right; Compose returns a reusable pipeline.
// Generic so non-string transform chains compose too.
func Apply[T any](value T, transforms ...func(T) T) T
func Compose[T any](transforms ...func(T) T) func(T) T

// e.g.
clean := sanitize.Compose(sanitize.Trim, strings.ToLower, sanitize.Collapse)
name  := clean("  Ann   Lee ")   // "ann lee"  (strings.ToLower composes directly)
```

**Sanitizers** (grouped; one-arg funcs compose via `Apply`/`Compose`):

```go
// whitespace / control
func Trim(s string) string            // strings.TrimSpace
func Collapse(s string) string        // trim + collapse internal whitespace runs → single space
func SingleLine(s string) string      // remove line breaks (CR/LF/Unicode) then Collapse
func NoSpaces(s string) string        // remove ALL whitespace
func StripControl(s string) string    // drop Unicode Cc/Cf control chars (incl. NUL), keep printable + normal spaces

// character-class filters
func KeepAlpha(s string) string        // letters + spaces only
func KeepDigits(s string) string       // 0-9 only
func KeepAlphanumeric(s string) string // letters + digits + spaces
func RemoveChars(s, chars string) string // delete every rune in chars

// HTML (escape/strip only — NOT a policy-based sanitizer)
func EscapeHTML(s string) string      // html.EscapeString
func UnescapeHTML(s string) string    // html.UnescapeString
func StripTags(s string) string       // remove <…> tags → plain text (extraction, NOT XSS-safe output)

// format canonicalizers (canonicalize, do NOT validate)
func Email(s string) string           // trim + lowercase (validation → validate.Email)
func Username(s string) string        // trim + lowercase + keep [a-z0-9._-], trim leading/trailing separators
func Filename(s string) string        // base name; strip path separators, control, leading dots ("" if nothing safe remains)
func HeaderValue(s string) string     // strip CR/LF + control (HTTP header/response-splitting guard)
func SanitizeURL(s string) string     // neutralize dangerous schemes (javascript:/data:/vbscript:) → "" ; else trimmed URL
```

**Decisions**

- **Composition is the headline pattern.** `Apply`/`Compose` (generic) are the reason
  sanitizers are all `func(string) string`; stdlib string funcs (`strings.ToLower`,
  `strings.TrimSpace`) drop into the same chain. This replaces the old package's
  per-field struct-tag pipeline with explicit, no-reflection composition.
- **HTML: escape or strip, never "sanitize a policy".** `EscapeHTML`/`UnescapeHTML`
  wrap stdlib; `StripTags` removes tags for *plain-text extraction* and is documented
  as **not** producing XSS-safe HTML (for safe output, escape). No bluemonday, no
  regex "PreventXSS" — those give false confidence at the wrong layer.
- `Email`/`Username`/`Filename` **canonicalize, they do not validate**; `Filename`
  strips `/` and `\` (via `strings`, OS-independent), control chars, and leading dots,
  returning `""` when nothing safe remains. `HeaderValue` removes `\r`/`\n`/control.
- `SanitizeURL` parses with `net/url`, lower-cases the scheme, and rejects any scheme
  outside an allowlist (`http`/`https`/`mailto`/relative) → `""` — the safe-link guard
  for user-supplied hrefs.
- **Deliberately NOT ported** (DNA violations / wrong package): the `sanitize:"…"`
  **struct-tag reflection DSL** + `SanitizeStruct`/`RegisterSanitizer` (no magic, no
  reflection — reflection lives only in `structfields`); **SQL string escaping**
  (`EscapeSQLString`/`RemoveSQLKeywords`/`SanitizeSQLIdentifier` — forge uses pgx
  parameterized queries); regex **injection "prevention"** (`PreventXSS`/`PreventLDAP`/
  shell-arg escaping — false security); **numeric** clamp/abs/round (not text; a future
  numeric helper); **slice/map** helpers (→ `slicex`/`set`); **PII formatting** (phone/
  SSN/credit-card/postal, `Mask*` → `stringsx.Mask`) and **path normalization** (→
  `path/filepath`). These are named here so the omission is a decision, not an oversight.

Deps: `strings`, `unicode`, `unicode/utf8`, `html`, `net/url`, `regexp` (compiled
tag-strip pattern).

---

## 3. `slug` — URL-safe slugs with options (recommended)

Ports the prior `pkg/slug` options API (the reference implementation at commit
`0b68f05`) onto forge conventions: NFKD folding via `x/text`, and random suffixes
drawn from the shipped `random` (not a private `crypto/rand`).

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
- **Random suffixes use `random`.** `WithSuffix`, `WithMinLength`, and the reserved-hit
  path draw a bias-free `[a-z0-9]` string from `random` (per-char `random.Int`), not a
  local `crypto/rand` with modulo bias as in the reference impl. Uppercase is added to
  the alphabet only when `WithLowercase(false)`.
- **`Unique` vs random suffixes.** `Unique(s, exists, …)` appends a human-friendly
  incrementing `-2`/`-3`/… (blog-post style) until the predicate clears — a different
  collision strategy from the random `WithSuffix`/`WithReservedSlugs`. Both ship.
- `slug` does **not** import `sanitize` (its transform is distinct). **`MakeLang`
  dropped for v1** (YAGNI); addable later without an API break.
- Non-Latin scripts with no ASCII fold collapse to `""` (callers fall back to an id);
  `WithMinLength`/`WithSuffix` still yield a random-only slug from an empty base.

Deps: `golang.org/x/text/unicode/norm`, forge `random`, `strings`, `unicode`, `unicode/utf8`.

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

There is **no `Validator` instance**. A *rule* is the exact `sanitize` analog —
`type Rule[T any] func(T) Violation` (the zero `Violation` means "passed", so
`func(T) Violation` ↔ `sanitize`'s `func(T) T`). **Param-less rules are used bare**
(`validate.Email`, `validate.Required`); rules that take parameters are tiny
constructors returning a `Rule[T]` (`validate.MinLen(2)`). `Apply` runs a field's
rules against its value; `Check` aggregates. Allocation-light — see below.

```go
// Param is one interpolation value. []Param (ordered, ~1 alloc, only on failure).
type Param struct {
    Key   string `json:"key"`
    Value any    `json:"value"`
}

// Violation is one failed check — a VALUE, never a heap pointer. The zero Violation
// (Key == "" && Message == "") means "passed". Field is filled in by Apply/Manual.
type Violation struct {
    Field   string  `json:"field,omitempty"`
    Key     string  `json:"key,omitempty"`     // "validation.required", "validation.between", …
    Params  []Param `json:"params,omitempty"`  // {min:18},{max:72}; nil when the rule has none
    Message string  `json:"message,omitempty"` // literal override (via Msg), already interpolated
}
func (v Violation) IsZero() bool     // true ⇒ the check passed
func (v Violation) String() string   // fmt.Stringer: Message when set, else Key

// Rule is the sanitize analog: tests a value, returns a Violation (zero = pass).
// A param-less rule IS a Rule[T] (used bare); a parameterized rule is a constructor
// that returns one.
type Rule[T any] func(value T) Violation

// ── Composition (the analog of sanitize.Apply/Compose) ──────────────────────────
type Result []Violation   // one field's violations; nil when the field is clean

// Apply runs each rule against value, tags failures with field, drops passes.
func Apply[T any](field string, value T, rules ...Rule[T]) Result
// Manual injects a literal / keyed violation (e.g. after a DB "email taken" check).
func Manual(field, message string) Result
func ManualKey(field, key string, params ...Param) Result
// Check flattens field results into one error; untyped nil when clean.
func Check(results ...Result) error

// Rule algebra — each returns a REUSABLE Rule[T] (predefine once → zero per-request alloc):
func And[T any](rules ...Rule[T]) Rule[T]            // all must pass; first failure wins
func Or[T any](key string, rules ...Rule[T]) Rule[T] // ok if ANY sub-rule passes
func Not[T any](r Rule[T], key string) Rule[T]       // invert
func Each[T any](r Rule[T]) Rule[[]T]                // apply r to every element (+ {index})
func Msg[T any](r Rule[T], template string) Rule[T]  // literal message, interpolating the rule's Params
func WithKey[T any](r Rule[T], key string) Rule[T]   // swap the i18n key
func When[T any](cond bool, r Rule[T]) Rule[T]       // conditional CHECK (passes when !cond)

func WhenField(cond bool, results ...Result) Result  // conditional FIELD/group (cond ? flatten : nil)

// Errors is the aggregated failure set Check returns.
type Errors []Violation
func (e Errors) Error() string                   // single-line, sorted, for logs
func (e Errors) ByField() map[string][]Violation // built on demand (JSON API responses)
```

```go
// A Rule[T] is func(T) Violation. PARAM-LESS rules are plain functions used BARE
// (validate.Email, validate.Required); PARAMETERIZED rules are constructors that
// return a Rule[T] (validate.MinLen(2)). Keys are "validation.<rule>"; {…} = Params.

// presence / length
func Required[T comparable](v T) Violation    // validation.required — bare (zero-value ⇒ fail)
func NotBlank(s string) Violation             // validation.not_blank — bare
func MinLen(n int) Rule[string]               // validation.min_len    {min}
func MaxLen(n int) Rule[string]               // validation.max_len    {max}
func LenBetween(min, max int) Rule[string]    // validation.len_between {min,max}

// string content
func Alpha(s string) Violation                // validation.alpha — bare
func Alphanumeric(s string) Violation         // validation.alphanumeric — bare
func Numeric(s string) Violation              // validation.numeric — bare
func ASCII(s string) Violation                // validation.ascii — bare
func Lowercase(s string) Violation            // validation.lowercase — bare
func Uppercase(s string) Violation            // validation.uppercase — bare
func Contains(sub string) Rule[string]        // validation.contains   {sub}
func HasPrefix(prefix string) Rule[string]    // validation.has_prefix {prefix}
func HasSuffix(suffix string) Rule[string]    // validation.has_suffix {suffix}
func Match(re *regexp.Regexp, key string) Rule[string] // caller-named key

// formats / encodings (all bare)
func Email(s string) Violation                // validation.email  (net/mail, no DNS)
func URL(s string) Violation                  // validation.url    (http/https absolute)
func UUID(s string) Violation                 // validation.uuid
func Slug(s string) Violation                 // validation.slug   (^[a-z0-9]+(-[a-z0-9]+)*$)
func Hex(s string) Violation                  // validation.hex
func Base64(s string) Violation               // validation.base64
func JSON(s string) Violation                 // validation.json   (json.Valid)

// network (all bare)
func IP(s string) Violation                   // validation.ip     (net.ParseIP)
func IPv4(s string) Violation                 // validation.ipv4
func IPv6(s string) Violation                 // validation.ipv6
func MAC(s string) Violation                  // validation.mac    (net.ParseMAC)
func Domain(s string) Violation               // validation.domain (hostname)

// ordered / numeric (generic; local unexported constraints à la typeconv)
func Min[T cmp.Ordered](min T) Rule[T]        // validation.min      {min}
func Max[T cmp.Ordered](max T) Rule[T]        // validation.max      {max}
func Between[T cmp.Ordered](min, max T) Rule[T] // validation.between {min,max}
func Positive[T number](v T) Violation        // validation.positive — bare (> 0)
func Negative[T number](v T) Violation        // validation.negative — bare (< 0)
func MultipleOf[T integer](n T) Rule[T]       // validation.multiple_of {n}

// equality / choice (generic)
func OneOf[T comparable](allowed ...T) Rule[T] // validation.one_of {allowed}
func Equal[T comparable](other T) Rule[T]      // validation.equal    (e.g. confirm-password)
func NotEqual[T comparable](other T) Rule[T]   // validation.not_equal

// time
func Before(u time.Time) Rule[time.Time]      // validation.before {other}
func After(u time.Time) Rule[time.Time]       // validation.after  {other}

// collections (generic)
func MinItems[T any](n int) Rule[[]T]         // validation.min_items {min}
func MaxItems[T any](n int) Rule[[]T]         // validation.max_items {max}
func UniqueItems[T comparable](items []T) Violation // validation.unique_items — bare
// Each (apply a rule to every element, + {index}) is a combinator, listed above.

// e-commerce / payments (all bare; checksum- or set-verified)
func CreditCard(s string) Violation           // validation.credit_card   (Luhn, 13–19 digits)
func CVV(s string) Violation                  // validation.cvv           (3–4 digits)
func CardExpiry(s string) Violation           // validation.card_expiry   (MM/YY, not in the past)
func CurrencyCode(s string) Violation         // validation.currency_code (ISO-4217 alpha-3 set)
func CountryCode(s string) Violation          // validation.country_code  (ISO-3166-1 alpha-2 set)
func EAN(s string) Violation                  // validation.ean           (EAN-8/13 check digit)
func ISBN(s string) Violation                 // validation.isbn          (ISBN-10/13 checksum)

// blockchain / crypto (all bare; checksum- or length-verified; stdlib — see EIP-55 note)
func ETHAddress(s string) Violation           // validation.eth_address    (0x + 40 hex; EIP-55 opt-in)
func BTCAddress(s string) Violation           // validation.btc_address    (Base58Check OR Bech32, verified)
func TronAddress(s string) Violation          // validation.tron_address   (Base58Check, 0x41; TRC-20 contracts share this)
func SolanaAddress(s string) Violation        // validation.solana_address (Base58 → exactly 32 bytes)
func Base58(s string) Violation               // validation.base58         (alphabet check)
func Bech32(s string) Violation               // validation.bech32         (BIP-173 polymod checksum)

// custom escape hatch: any func(T) Violation literal IS a Rule[T]; or wrap a predicate:
func Is[T any](pred func(T) bool, key string) Rule[T] // caller predicate + key
```

Usage — no instance; value written once per field; param-less rules bare:

```go
// A reusable rule set — built once, zero per-request allocation:
var ageRules = []validate.Rule[int]{validate.Required[int], validate.Between(18, 72)}

err := validate.Check(
    validate.Apply("age",   age,   ageRules...),
    validate.Apply("name",  name,  validate.NotBlank, validate.MinLen(2)),
    validate.Apply("email", email, validate.Msg(validate.Email, "invalid {field}")),
    validate.Manual("email", "Email is taken"),   // literal, e.g. after a uniqueness check

    // conditional / optional:
    validate.Apply("website", website, validate.When(website != "", validate.URL)),   // optional-if-present
    validate.Apply("state",   state,   validate.When(country == "US", validate.Required[string])),
    validate.WhenField(accountType == "business",                       // whole group, businesses only
        validate.Apply("company", company, validate.NotBlank),
        validate.Apply("vat",     vat,     validate.Required[string]),
    ),

    validate.Apply("tags", tags, validate.Each(validate.NotBlank)),     // a rule per element
)
return err   // untyped nil when clean, else Errors ([]Violation)
```

**Per-rule error structure (the Key + Params contract — stable public API).** The
authoritative list is the inline `validation.<rule>` key + `{params}` on each rule
above; a representative slice:

| Rule | `Key` | `Params` |
|---|---|---|
| `Required` / `NotBlank` | `validation.required` / `validation.not_blank` | — |
| `MinLen(n)` / `MaxLen(n)` | `validation.min_len` / `validation.max_len` | `{min}` / `{max}` |
| `Between(min,max)` | `validation.between` | `{min, max}` |
| `OneOf(a…)` | `validation.one_of` | `{allowed}` (slice) |
| `Contains(sub)` / `MultipleOf(n)` | `validation.contains` / `validation.multiple_of` | `{sub}` / `{n}` |
| `Each(rule)` | wrapped rule's key | `{index}` (of first failure) |
| `Match(re,key)` / `Is(pred,key)` | caller's `key` | — |

So `validate.Msg(validate.Between(18, 72), "Your age must be between {min} and {max}")` is a
`Rule[int]` that, on an out-of-range `age`, yields
`Violation{Key:"validation.between", Params:[{min 18},{max 72}], Message:"Your age must be between 18 and 72"}`
— and passes straight through (zero `Violation`) when `age` is in range.

**Decisions**

- **i18n keys are the public contract.** The `validation.<rule>` namespace and each
  rule's `Params` keys are documented and stable so i18n catalogs can target them.
  `Match`/`Is` take a caller-supplied key (there is no default sentence to emit).
- **No instance — a rule is `Rule[T] = func(T) Violation`.** Param-less rules are plain
  functions used **bare** (`validate.Email`); parameterized rules are tiny constructors
  returning a `Rule[T]` (`validate.MinLen(2)`). `Apply(field, value, rules…)` runs each
  rule against the value (written once), tags failures with the field, drops passes;
  `Check(results…)` flattens to an `Errors` (untyped `nil` when clean); `Manual`/`ManualKey`
  inject literal violations. This is the exact `sanitize` parallel (`func(T) Violation` ↔
  `func(T) T`); rules compose into rules via the algebra (`And`/`Or`/`Not`/`Each`/`Msg`/
  `WithKey`), so a `[]Rule[T]` can be predefined once and reused.
- **Generic vs bare inference.** Non-generic rules (`Email`, `NotBlank`, every string/
  crypto rule) are used fully bare anywhere. Generic param-less rules (`Required`,
  `Positive`, `Negative`, `UniqueItems`) are bare where `T` is inferable (e.g. inside
  `Apply`, from the value) and carry an explicit arg otherwise (`validate.Required[string]`,
  e.g. inside `When`). Either way there are **no call parens** on a param-less rule.
- **Conditional validation is two gates, not a DSL.** `When(cond, rule)` is a `Rule[T]`
  combinator — `rule` when `cond`, else a pass — so the guarded rule is only *evaluated*
  when `cond` holds (no wasted work; deferring the rule removes the eager-evaluation caveat
  the value form had). `WhenField(cond, results…)` includes or omits whole fields/groups.
  Together they cover country/account-type-dependent fields and *optional-if-present*
  (`When(value != "", rule)`) inside the declarative `Check(…)`. Anything richer is a plain
  Go `if` building `[]Result` — no extra API — which is why conditionals stay this small.
- **Allocation profile.** A param-less rule used bare (`validate.Email`) is a plain
  function value — **no allocation**. A parameterized rule (`validate.MinLen(2)`) or a
  combinator (`And`/`Msg`/`Each`/…) returns a small closure: **~1 alloc when built inline,
  zero when the `[]Rule[T]` is predefined** (built once at init, reused per request — the
  recommended hot-path pattern). `Apply`/`Check` take non-escaping variadics that stack-
  allocate, and a passing rule returns the zero `Violation` value. So valid input with
  bare/predefined rules **allocates nothing**; heap use is confined to the failure path
  (the failing rule's `[]Param` + the aggregated slices), where an error response is being
  built anyway. `[]Param` (not a map) keeps that path to ~1 alloc per violation; a hot loop
  can reuse one `Errors` buffer.
- **Overrides are `Rule[T]` combinators.** `Msg(rule, template)` and `WithKey(rule, key)`
  wrap a rule; the returned rule runs the inner one and, **only on failure**, interpolates
  the check's `Params` into `{name}` placeholders (dependency-free scan of `[]Param`;
  unknown placeholder left verbatim so typos stay visible) into `Message`, or swaps `Key`.
  Wrapping the rule (not a value) means they apply to every rule uniformly and nest inside
  `And`/`Or`/`Each`.
- `Required` uses zero-value comparison of a `comparable` (so `""`, `0`, zero-time
  are "missing"); whitespace-only strings pass `Required` but fail `NotBlank`.
- `Email` = `net/mail.ParseAddress` + a light structural check (no DNS). `URL` =
  `net/url.Parse` requiring an `http`/`https` scheme and a host.
- **Format rules prefer stdlib parsers over regex** where one exists (correctness over
  a hand-rolled pattern): `IP`/`IPv4`/`IPv6`→`net.ParseIP`, `MAC`→`net.ParseMAC`,
  `JSON`→`json.Valid`, `Hex`→`encoding/hex`, `Base64`→`encoding/base64`. `UUID`/`Slug`/
  `Domain`/`Alpha`… use small precompiled `regexp` patterns. All emit `validation.<rule>`.
- **Numeric/collection rules are generic and stdlib-only.** `Min`/`Max`/`Between` use
  `cmp.Ordered`; `Positive`/`Negative` (`number`) and `MultipleOf` (`integer`) use local
  unexported constraints mirroring `typeconv` (no `x/exp/constraints` dep). `MinItems`/
  `MaxItems`/`UniqueItems`/`Each` operate on `[]T`; `Each` runs a per-element rule and
  reports the first failing element's `Violation` with an added `{index}` param.
- **E-commerce & crypto rules verify checksums/sets, not just shape.** `CreditCard`
  runs the Luhn algorithm; `EAN`/`ISBN` verify the check digit; `CVV`/`CardExpiry` are
  structural + not-expired (via `time`). `CurrencyCode`/`CountryCode` check membership
  in compact **bundled ISO-4217 / ISO-3166-1 code sets** — `validate` embeds only the
  *code set* while `money` holds the authoritative currency *metadata*; the small,
  deliberate duplication keeps `validate` a dependency-free leaf. `BTCAddress` verifies
  **Base58Check** (double-`crypto/sha256`) or **Bech32** (BIP-173 polymod), with
  `Base58`/`Bech32` exposed as the reusable primitives under it — all stdlib.
  `TronAddress` is Base58Check with a `0x41` version byte (34-char `T…`; **TRC-20 token
  contracts share the TRON account address format**, so this covers both). `SolanaAddress`
  is plain Base58 that must decode to **exactly 32 bytes** (an Ed25519 public key) — Solana
  has no address checksum, so decoded length is the check; an on-curve test is deliberately
  skipped since valid program-derived addresses (PDAs) are off-curve. Both reuse the
  `Base58` primitive — no new deps.
- **`ETHAddress` is structural by design (`0x` + 40 hex, any case).** Full **EIP-55
  mixed-case checksum verification is deliberately NOT in `validate` core** — it needs
  Keccak-256 (`golang.org/x/crypto/sha3`), and taking that dep would end `validate`'s
  stdlib-only status for one rule. A caller needing EIP-55 verification does it at the
  wallet layer (or a future `cryptoaddr` helper that owns the x/crypto edge). This is
  the one scoping trade-off worth flagging.
- `Violation` implements `fmt.Stringer` — `String()` returns `Message` when set,
  otherwise `Key` — so a violation is always printable (logs, `%v`, quick debug)
  without an i18n layer. `Errors.Error()` builds on this.
- `Errors` (a `[]Violation`) marshals to JSON as an array of `{field, key, params?,
  message?}`, with `ByField()` offering the `field → [violations]` shape API clients
  often want (built on demand); `Error()` renders a sorted single line (log-friendly).
  All of `validate` is **stateless free functions**, so it is inherently goroutine-safe
  (no per-request instance to misuse).
- **Out of scope:** struct-tag binding (→ `structfields` + these rules); *key-based*
  message rendering (→ future `i18n`; `validate` only emits Key+Params) — note the
  *literal* `Msg` template is interpolated in-package, so it needs no i18n layer;
  cross-field rules beyond what `Equal`/`NotEqual`/`Before`/`After`/`Manual`/`True`/
  closures express; phone/SSN/VAT and locale-specific postal codes (domain/locale-
  specific); Ethereum EIP-55 checksum (needs Keccak — kept out of the stdlib-only core).

Deps: `regexp`, `net`, `net/mail`, `net/url`, `crypto/sha256`, `math/big`,
`encoding/hex`, `encoding/base64`, `encoding/json`, `strings`, `unicode`,
`unicode/utf8`, `cmp`, `time`, `sort`, `fmt`.

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
| `sanitize` | — | strings, unicode, unicode/utf8, html, net/url, regexp |
| `slug` | **golang.org/x/text/unicode/norm** | forge `random`; strings, unicode, unicode/utf8 |
| `structfields` | — | reflect, strings, errors, fmt |
| `validate` | — | regexp, net, net/mail, net/url, crypto/sha256, math/big, encoding/{hex,base64,json}, strings, unicode, cmp, time, sort, fmt |
| `decimal` | — | math/big, strings, strconv, errors, fmt |
| `money` | forge `decimal` | strings, errors, fmt |
| `filetype` | — | bytes, io, net/http, strings, errors |

Forge edges inside the batch: `money → decimal`, `slug → random`. Only external
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
- `sanitize`: rich-HTML/policy sanitization (bluemonday); `Truncate`/`Mask`/case
  conversion (→ `stringsx`); struct-tag reflection DSL (`SanitizeStruct`); SQL/LDAP/
  shell "injection prevention"; numeric clamp (→ future numeric helper); slice/map
  helpers (→ `slicex`/`set`); PII/locale formatting (phone/SSN/credit-card/postal).
- `slug`: per-language transliteration (`MakeLang`).
- `structfields`: embedded-struct flattening; struct-tag *binding* (→ consumers).
- `validate`: message rendering (→ `i18n`); struct-tag DSL.
- `decimal`: scientific-notation parsing; arbitrary-precision "exact" division.
- `money`: FX conversion; locale-aware formatting.
- `filetype`: OOXML sub-type disambiguation; deep container parsing.
