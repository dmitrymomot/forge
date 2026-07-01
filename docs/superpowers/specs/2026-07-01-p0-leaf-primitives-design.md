# Design: P0 leaf primitives — `typeconv`, `iox`, `bufpool`, `nullx`, `bytesize` (5 packages)

## Overview

Second batch of P0 foundation packages, following the generic-leaf batch
(`slicex`/`ptr`/`mapx`/`set`/`encoding`/`enum`/`stringsx`). With P1 crypto complete,
these fill remaining gaps in the P0 layer.

Five stdlib-only leaf packages with **zero forge dependencies**:

| Package | Tier | Purpose |
|---|---|---|
| `typeconv` | **core** | Reflection-free scalar string ⇄ Go coercion (`Parse`/`Format`). The substrate `envconfig`/form/featureflag build field decoders on. |
| `iox` | recommended | Small `io.Reader`/`Writer`/`Closer` shims every streaming package reuses. |
| `bufpool` | recommended | One shared, size-capped `*bytes.Buffer` pool, replacing per-package private pools. |
| `nullx` | recommended | Generic `Null[T]` that round-trips through `database/sql` **and** encoding/json (as JSON `null`). |
| `bytesize` | recommended | Parse/format human byte sizes; a `ByteSize` type that drops into env-tagged config. |

`typeconv` is the sole core-tier package and the highest-leverage item: it unblocks
`envconfig`, form decoding, and featureflag, none of which can stay reflection-honest
without it. The other four are shared ergonomics consumed across the web, data, and
render layers.

## Design DNA (applies to every package)

- **stdlib-only, zero forge deps.** Each is an independent leaf; build order is free.
- **Idiom:** stateless free-funcs and/or generic value types. None of these is a
  configured service, so there is **no `New(...Option)`/`Config`** and **no builders**.
- **Anatomy:** `doc.go` (package doc; a runnable `Example` is optional and, per
  existing P0-leaf precedent, usually omitted), `errors.go` (`errors.Is`-matchable
  single-line sentinels) where the package defines sentinels, plus impl file(s).
  Keep files flat and single-responsibility.
- **Go 1.26.** Use the `new(expr)` builtin where a pointer literal is needed (no
  `ptr.To` wrapper); run `just` `modernize`/`lint` before declaring done.
- **Public methods never return unexported types.**
- **Black-box tests only** (`package X_test`), table-driven.

---

## 1. `typeconv` — scalar string ⇄ Go coercion (core)

The reflection-free scalar substrate. **Scalar-at-a-time only** — walking struct fields
is `structfields`' job; `typeconv` parses one value at a time. This keeps the framework's
"one sanctioned reflection site" rule intact: `structfields` reflects, `typeconv` does not.

```go
// Generic dispatch over base kinds.
func Parse[T any](s string) (T, error)
func Format(v any) string

// Constraint-based helpers (handle ~defined types via generics).
func ParseBool(s string) (bool, error)
func ParseInt[T signed](s string) (T, error)
func ParseUint[T unsigned](s string) (T, error)
func ParseFloat[T float](s string) (T, error)
func ParseDuration(s string) (time.Duration, error)
func ParseTime(s string) (time.Time, error)          // RFC3339

// List coercion (Bucket B addition — kills split-then-parse duplication in
// envconfig/form/featureflag).
func ParseSlice[T any](s, sep string) ([]T, error)

// sentinels
var ErrUnsupportedType = errors.New("typeconv: unsupported type")
var ErrSyntax          = errors.New("typeconv: invalid syntax")   // wraps strconv/time errors
```

Local (unexported) constraints — callers instantiate freely (`ParseInt[MyID]`) without
the constraint being exported:

```go
type signed   interface{ ~int | ~int8 | ~int16 | ~int32 | ~int64 }
type unsigned  interface{ ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr }
type float    interface{ ~float32 | ~float64 }
```

**Decisions**

- **`Parse[T]` dispatches on the *exact* base kind** via `switch any(zero).(type)`. It
  supports `string`, `bool`, the sized int/uint kinds, `float32/64`, `time.Duration`,
  and `time.Time`. A **defined** type will **not** match generic `Parse`: numeric
  defined types (`type Port int`) are served by the constraint helpers (`ParseInt[Port]`);
  string-defined types (`type Status string`) take a trivial explicit conversion
  (`Status(s)`). This is a documented, deliberate limitation that keeps `Parse`
  reflection-free. `time.Duration` is matched **before** `int64` in the switch (it is a
  distinct named type).
- **Errors wrap.** A parse failure returns `fmt.Errorf("%w: %v", ErrSyntax, err)` so both
  `errors.Is(err, ErrSyntax)` and the underlying strconv/time detail survive. An
  unsupported `T` returns `fmt.Errorf("%w: %T", ErrUnsupportedType, zero)`.
- **`Format` is the lossless inverse** for the same scalar set (`strconv.Format*` with
  `-1` float precision, `bool`→`"true"/"false"`, `Duration.String()`,
  `Time.Format(RFC3339)`). Unsupported types format via `fmt.Sprint` as a last resort
  (never an error, since `Format` returns a bare string).
- **`ParseSlice`** trims the whole input, returns `nil, nil` for empty input, otherwise
  splits on `sep`, trims each element, drops empty-after-trim elements (so `"1, 2, 3,"`
  → `[1 2 3]`), and `Parse[T]`s each. A bad element returns the wrapped `ErrSyntax`.
- **Time is RFC3339 both ways.** Date-only / naive-datetime layouts are intentionally
  not guessed now; a `ParseTimeIn(layouts...)` can be added later if a consumer needs it.

**Out of scope:** struct-tag binding (→ `structfields` + these), locale-aware parsing
(→ `i18n`), collection types beyond flat slices.

---

## 2. `iox` — io shims (recommended)

Small `io` helpers every streaming package reuses. Unbuffered wrappers only — `bufio`
already exists and is not duplicated.

```go
// Errors on the byte past the limit (io.LimitReader silently truncates,
// which produces a wrong/ambiguous result for 413 semantics).
func LimitReader(r io.Reader, n int64) io.Reader

// io.Copy(io.Discard, rc) then rc.Close() — lets an HTTP client reuse the
// keep-alive connection. Returns the first non-nil of the copy/close errors.
func DrainClose(rc io.ReadCloser) error

// Closes every closer, aggregating failures via errors.Join.
func MultiCloser(closers ...io.Closer) io.Closer

// Wraps an io.Writer and counts bytes written.
type CountingWriter struct{ /* w io.Writer; n int64 */ }
func NewCountingWriter(w io.Writer) *CountingWriter
func (c *CountingWriter) Write(p []byte) (int, error)
func (c *CountingWriter) N() int64

// io.WriteCloser whose Close is a no-op.
func NopWriteCloser(w io.Writer) io.WriteCloser

var ErrLimitExceeded = errors.New("iox: read limit exceeded")
```

**Decisions**

- `LimitReader` allows exactly `n` bytes; the read that would exceed `n` returns
  `ErrLimitExceeded` (not `io.EOF`). Callers map that to HTTP 413.
- `MultiCloser` closes **all** closers even if an early one fails, then returns the
  joined error.

---

## 3. `bufpool` — shared `*bytes.Buffer` pool (recommended)

One package-level `sync.Pool` of `*bytes.Buffer`, so transactional renderers/encoders
stop each defining a private `getBuf/putBuf` (today: `render`; soon: sse, emailtemplate,
`sign`/`token` build steps).

```go
func Get() *bytes.Buffer                     // returned already Reset
func Put(b *bytes.Buffer)                     // dropped (not pooled) if cap > maxCap
func Do(fn func(*bytes.Buffer) error) error   // borrow → reset → run → return (panic-safe)

const maxCap = 64 << 10 // 64 KiB — do not pin oversized buffers in the pool
```

**Decisions**

- **Zero-config on purpose.** The 64 KiB cap-drop threshold is a tuned constant, not an
  option — this is exactly what distinguishes `bufpool` from a generic `pool[T]` (a
  separate future primitive, deliberately not built here).
- `Get` returns a `Reset` buffer; `Put` drops any buffer whose `Cap()` exceeds `maxCap`
  so a one-off large render doesn't pin memory in the pool.
- `Do` returns the buffer to the pool in a `defer`, so it is reclaimed even if `fn`
  panics.

---

## 4. `nullx` — `Null[T]` for SQL + JSON null (recommended)

A single generic nullable that round-trips through **both** `database/sql` and
encoding/json, replacing the `sql.NullString`/`NullInt64`/… family. Implemented by
**wrapping stdlib `sql.Null[T]`** (Go 1.22+), inheriting its hardened `Scan`/`Value`
(`convertAssign`) and adding JSON-as-`null`.

```go
type Null[T any] struct {
    sql.Null[T] // promotes .V, .Valid, Scan, Value
}

func Of[T any](v T) Null[T]        // {V: v, Valid: true}
func Empty[T any]() Null[T]         // {Valid: false}

func (n Null[T]) Get() (T, bool)    // (V, Valid)
func (n Null[T]) Ptr() *T           // nil when !Valid, else a pointer to a copy of V
func FromPtr[T any](p *T) Null[T]   // nil → Empty, else Of(*p)

func (n Null[T]) MarshalJSON() ([]byte, error)   // !Valid → `null`, else json.Marshal(n.V)
func (n *Null[T]) UnmarshalJSON(b []byte) error  // `null` → Valid=false; else decode into V, Valid=true
```

**Decisions**

- **Wrap, don't hand-roll.** We inherit `sql.Null[T]`'s conversion — and its limits: `T`
  must be a kind `convertAssign` supports (scalars, `time.Time`, `[]byte`, `string`).
  A JSON-column-backed `Null[SomeStruct]` is **out of scope**; that wants its own
  `sql.Scanner`. This is the direct trade-off of choosing the DRY route.
- **Self-contained `Ptr`/`FromPtr`.** `nullx` does **not** import `ptr` — the pointer
  helpers are trivial and keeping them local removes a dependency edge. `Ptr()` returns a
  pointer to a *copy* of `V` (never an alias into the struct).
- Distinct from `ptr.Optional[T]`: `Optional` models JSON PATCH *absence* (present-but-
  null vs. absent); `Null[T]` models a SQL/JSON *null value*. They do not overlap.

---

## 5. `bytesize` — human byte sizes (recommended)

Parse/format `"10MB"`/`"1.5GiB"` and a `ByteSize` type implementing `TextUnmarshaler`
so it drops into env-tagged `Config` and JSON. Powers upload/body-limit config.

```go
type ByteSize int64

const (
    B  ByteSize = 1
    KB          = 1000 * B     // SI (powers of 1000)
    MB          = 1000 * KB
    GB          = 1000 * MB
    TB          = 1000 * GB
    PB          = 1000 * TB
    KiB ByteSize = 1024 * B    // IEC (powers of 1024)
    MiB          = 1024 * KiB
    GiB          = 1024 * MiB
    TiB          = 1024 * GiB
    PiB          = 1024 * TiB
)

func Parse(s string) (ByteSize, error)

func (b ByteSize) String() string          // IEC by default (see below)
func FormatSI(b ByteSize) string           // KB/MB/GB…
func FormatIEC(b ByteSize) string          // KiB/MiB/GiB…

func (b ByteSize) MarshalText() ([]byte, error)   // == String()
func (b *ByteSize) UnmarshalText(p []byte) error   // == Parse

var ErrInvalidSize = errors.New("bytesize: invalid size")
```

**Decisions**

- **SI/IEC split (locked):** `KB=1000`, `KiB=1024`, etc. `Parse` accepts either family
  case-insensitively (`"kb"`, `"KiB"`), an optional trailing `B`, optional whitespace
  (`"10 MB"`), and a bare number as raw bytes. Unknown suffixes → `ErrInvalidSize`.
- **Formatting defaults to IEC** (`String`/`MarshalText`), because framework config
  (memory, upload/body caps) is mentally binary. It picks the largest unit `≤ |b|` and
  prints up to a few decimals, trimming trailing zeros (`10MiB`, `1.5GiB`), so the output
  **round-trips** back through `Parse`. `FormatSI`/`FormatIEC` force a family explicitly.
- **No bit units.** `Kb`(kilobit) is intentionally excluded — it invites the exact
  `Kb`/`KB` confusion the SI/IEC split exists to avoid, and every framework consumer is
  byte-denominated.
- **Range:** consts stop at `PB`/`PiB`; `int64` comfortably holds these (max ≈ 8 EiB).

---

## Cross-cutting concerns

### Dependencies

None on forge; stdlib per package:

- `typeconv`: `strconv`, `time`, `strings`, `errors`, `fmt`.
- `iox`: `io`, `errors`.
- `bufpool`: `bytes`, `sync`.
- `nullx`: `database/sql`, `encoding/json`, `errors`.
- `bytesize`: `strconv`, `strings`, `errors`, `math`.

All five are independent leaves and can be built in any order or in parallel.

### Build approach

TDD each package (tests first, per the generic-leaf batch). Because there are no
cross-dependencies, the five can be implemented as five independent tasks. Run
`just check` (fmt + lint + test) and `modernize` before declaring any package done.

### Testing (black-box, `package X_test`)

- `typeconv`: table-driven round-trips (`Parse`→`Format`→`Parse`) across every supported
  kind; defined-type behavior of `Parse` vs. the constraint helpers; `ParseSlice`
  trimming/empty/trailing-sep and element-error cases; `ErrUnsupportedType`/`ErrSyntax`
  matching via `errors.Is`.
- `iox`: `LimitReader` at exactly-limit and over-limit (asserts `ErrLimitExceeded`, not
  EOF); `DrainClose` consumes + closes; `MultiCloser` closes all and joins errors;
  `CountingWriter.N()`; `NopWriteCloser.Close()` is a no-op.
- `bufpool`: `Get` returns a reset buffer; `Put` drops an oversized buffer (assert it is
  not handed back); `Do` returns the buffer even when `fn` panics; a `-race` concurrency
  test.
- `nullx`: SQL scan round-trip through a fake `driver.Value` (and a `sql.Null[T]`-backed
  path); JSON marshal `Empty`→`null` and value→encoded; `UnmarshalJSON("null")` clears
  `Valid`; `Ptr`/`FromPtr` nil handling and copy-not-alias.
- `bytesize`: `Parse`/`String` round-trip table across SI and IEC, whitespace, case,
  bare numbers, decimals; `ErrInvalidSize` for junk suffixes; `MarshalText`/
  `UnmarshalText` via a struct field.

### Top risks

1. **`nullx` embedding.** Confirm `sql.Null[T]` promotes both `Scan` (pointer receiver)
   and `Value` (value receiver) through the embed, and that adding our
   `MarshalJSON`/`UnmarshalJSON` introduces no method conflict (`sql.Null[T]` implements
   neither). Mitigate with the scan + json round-trip tests above.
2. **`typeconv.Parse[T]` exact-kind dispatch.** Defined types silently fall to the
   `default`/`ErrUnsupportedType` branch — verify the documented split (generic `Parse`
   for base kinds, constraint helpers for defined kinds) holds and is asserted in tests.
3. **`bytesize` round-trip fidelity.** Decimal formatting (`1.5GiB`) must parse back to
   the same `int64`; test the `String`→`Parse` identity explicitly, including values that
   are not clean unit multiples.
4. **`bufpool` panic safety.** `Do` must return the buffer on panic; assert with a
   recovering test.

## Out of scope (deliberate)

- `typeconv`: struct-tag binding (reflection lives only in `structfields`), locale-aware
  parsing (`i18n`).
- `iox`: buffering (`bufio`), re-wrapping stdlib `TeeReader`/`io.NopCloser`.
- `bufpool`: a generic `pool[T]` (a separate future primitive); non-`bytes.Buffer` types.
- `nullx`: hand-rolled `convertAssign`; `Null[T]` over JSON columns / arbitrary structs.
- `bytesize`: bit units; anything but SI/IEC byte scales.
