# Design: `id` + `ctxkey` (P0 primitives)

- **Date:** 2026-07-01
- **Status:** Draft for review
- **Scope:** Two flat top-level P0 primitives from `docs/maximal-package-set.md`:
  `id` (the framework-mandated unique-ID source) and `ctxkey` (the typed context-key seam).
  Both are stdlib-first, zero third-party dependencies. `id` depends only on the shipped
  `clock` package; `ctxkey` depends only on `context`. This spec builds the ID schemes,
  value types, generation APIs, and the context-key primitive — it does **not** build any
  downstream consumer (`requestid`, `session`, `tenant`, the pgx type-registration, etc.).

## Overview

`id` is mandated framework-wide: `README.md` and `CONTRIBUTING.md` both require that **all
IDs be generated through `pkg/id/` exclusively** — yet the package does not exist. This spec
fills that gap and, per the project decision, does so with **three distinct ID schemes** in
one package rather than a single unified format:

| Type    | Backing    | Bit layout                          | String form                     | Length   |
|---------|------------|-------------------------------------|---------------------------------|----------|
| `UUID`  | `[16]byte` | 48-bit ms ts + version/variant + 74-bit rand (UUIDv7, RFC 9562) | canonical hex `0189…-…` | 36 chars |
| `ULID`  | `[16]byte` | 48-bit ms ts + 80-bit rand          | Crockford base32                | 26 chars |
| `Short` | `[10]byte` | 48-bit ms ts + 32-bit rand          | Crockford base32                | 16 chars |

All three carry a **big-endian millisecond timestamp prefix**, so byte order equals time
order and the string forms are lexicographically sortable. `Short` is the compact,
URL-friendly, still-sortable option for link shorteners and similar; `ULID` is the standard
26-char sortable ID; `UUID` (v7) is for native Postgres `uuid` columns and cross-system
interop.

`ctxkey` is the typed, collision-free context-key primitive every request-scoped package
adopts (`requestid`, `session`, `authmw`, `tenant`) instead of hand-rolling the
`type key struct{}` + `WithX`/`XFrom` boilerplate and re-doing a `ctx.Value(...).(T)`
assertion at every read.

Both packages are **allocation-conscious by design**, with allocation invariants asserted in
tests (via `testing.AllocsPerRun`) and a full benchmark suite.

### Representative usage

```go
import (
	"github.com/dmitrymomot/forge/ctxkey"
	"github.com/dmitrymomot/forge/id"
)

// id — common path: zero-arg, lock-free, crypto/rand, system clock.
u := id.NewUUID()          // id.UUID  → "0190f8c2-...-...", native Postgres uuid
l := id.NewULID()          // id.ULID  → "01J9Z3K..." (26 chars, sortable)
s := id.NewShort()         // id.Short → 16-char sortable, URL-safe
link := s.StringLower()    // lowercase for a shortener URL

// id — monotonic generator with an injected clock (deterministic tests / strict ordering).
g := id.NewGenerator(id.WithClock(clk), id.WithMonotonic())
a, b := g.Short(), g.Short()   // strictly increasing within the same millisecond

// ctxkey — declare once, typed accessors forever.
var userKey = ctxkey.New[User]("user")
ctx = userKey.With(ctx, currentUser)
u2, ok := userKey.From(ctx)     // (User, bool) — no assertion at the call site
```

## Design DNA

Both packages follow the framework conventions: flat layout (files in the package dir, no
nested folders), stdlib-only production code, free functions where natural, options (not
builders) for the `Generator`, `errors.Is`-matchable sentinels, and **black-box tests only**
(`package id_test` / `package ctxkey_test`).

---

## Package `id`

### Value types

`UUID` and `ULID` have underlying type `[16]byte`; `Short` is `[10]byte` (80 bits → exactly
16 Crockford base32 chars, no padding). Being fixed-size arrays, they are copied by value and
**generation touches no heap** — only stringification allocates.

Each type implements the same method set:

```go
func (x T) String() string              // spec-canonical rendering; encodes into a stack array, one alloc
func (x T) StringLower() string         // lowercase variant
func (x T) StringUpper() string         // uppercase variant
func (x T) Time() time.Time             // extract the embedded ms timestamp; no re-parse
func (x T) IsZero() bool

func (x T) MarshalText() ([]byte, error)   // encoding.TextMarshaler (JSON comes free via this)
func (x *T) UnmarshalText(b []byte) error  // encoding.TextUnmarshaler; case-insensitive

func (x T) Value() (driver.Value, error)   // database/sql/driver.Valuer → canonical string
func (x *T) Scan(src any) error            // sql.Scanner ← string | []byte; case-insensitive

// package-level parsers
func ParseUUID(s string) (UUID, error)
func ParseULID(s string) (ULID, error)
func ParseShort(s string) (Short, error)
```

**Canonical rendering per scheme** (`String()`):
- `UUID` → **lowercase** hex `8-4-4-4-12` (RFC 9562 canonical).
- `ULID` / `Short` → **uppercase** Crockford base32 (ULID-spec canonical).

`StringLower()` / `StringUpper()` provide the alternate case on all three. `MarshalText` and
`Value` use `String()` (canonical) so DB and JSON representations are stable; `UnmarshalText`,
`Scan`, and the `Parse*` functions are **case-insensitive** and map Crockford's ambiguous
input characters (`I`/`L` → `1`, `O` → `0`), so any-case input round-trips.

### Encoding — hand-rolled for zero-alloc

A single internal Crockford base32 codec (alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ`,
excluding `I`, `L`, `O`, `U`) encodes and decodes **directly between the fixed `[N]byte` and
the destination buffer** — no `encoding/base32`, no `math/big`, no `fmt`. `UUID.String()`
writes the 36-char hex form into a `[36]byte` stack array via a nibble lookup. Decoding
validates length and alphabet, returning `ErrMalformed` on any violation.

### Postgres / `database/sql` binding

- `UUID.Value()` returns the **canonical 36-char string**. Both pgx and lib/pq accept a
  string for a `uuid` column, parse it client-side, and store it as a **native 16-byte
  `uuid`** (not text, not bytea). Returning raw `[]byte` is rejected because lib/pq encodes
  byte slices as `bytea`, which fails against a `uuid` column.
- `UUID`'s underlying `[16]byte` makes a future **pgx binary-path type registration**
  (implementing pgx's `UUIDValuer` to skip the per-bind string parse) a zero-copy
  `[16]byte(u)`. That registration is **out of scope here** — it belongs in the `postgres`
  data package, because `id` is a P0 primitive and must stay pgx-free.
- PG 18's server-side `uuidv7()` is orthogonal: forge generates IDs **client-side** (to
  reference an ID before INSERT, and because ULID/Short have no server equivalent). Both
  store into the identical native `uuid` column; compatible with PG ≤17 and PG 18.
- `ULID.Value()` / `Short.Value()` return their base32 string → a `text`/`char(N)` column.
  (`ULID`'s 16 bytes could alternatively live in a `bytea`/`uuid` column; not the default —
  callers who want that convert explicitly.)

### UUIDv7 bit correctness

`UUID` sets the version nibble to `0b0111` (7) and the variant bits to `0b10` per RFC 9562:
48-bit big-endian `unix_ts_ms`, 4-bit version, 12-bit random (`rand_a`), 2-bit variant,
62-bit random (`rand_b`) — 74 random bits total. `Time()` reads the leading 48 bits.

### Generation — free functions + `Generator`

**Free functions** (common path — zero-arg, lock-free, fresh `crypto/rand` read into a stack
buffer, `clock.System()`). They delegate to an internal package-default `Generator` that is
**non-monotonic** (no mutex → best throughput; this is the "default = fastest" choice):

```go
func NewUUID() UUID
func NewULID() ULID
func NewShort() Short
```

Generation reads `crypto/rand` **directly** (not via `randx.Bytes`, which allocates a slice) —
a deliberate deviation from "reuse randx" in service of the zero-alloc path. A broken OS RNG
is unrecoverable, so on `crypto/rand` failure the constructors **panic** (matching `randx`'s
stance); there are no error-returning variants.

**`Generator`** — for strict same-millisecond ordering and/or an injected clock:

```go
type Generator struct { /* clock, monotonic state, mutex */ }
type Option func(*Generator)

func NewGenerator(opts ...Option) *Generator
func WithClock(c clock.Clock) Option   // deterministic tests
func WithMonotonic() Option            // strictly-increasing within one ms

func (g *Generator) UUID() UUID
func (g *Generator) ULID() ULID
func (g *Generator) Short() Short
```

The constructor is named `NewGenerator` (not `New`) so it does not collide with the scheme
constructors; there is intentionally no bare `id.New()` (ambiguous across three schemes).

**Monotonic mode:** within the same millisecond, the random component is **incremented with
carry** instead of redrawn, guaranteeing strictly increasing, collision-free IDs from that
generator. On the (practically impossible) overflow of the random space inside one ms, the
generator advances the timestamp by 1 ms and redraws (documented behavior). This requires a
mutex, hence opt-in. `Short` benefits most: its 32-bit random has real same-ms collision odds
under heavy load that monotonic mode eliminates.

### Errors

```go
var ErrMalformed = errors.New("id: malformed")
```

`Parse*` and `Scan` wrap it with detail (e.g. `fmt.Errorf("id: bad ULID length %d: %w", n,
ErrMalformed)`); callers match with `errors.Is(err, id.ErrMalformed)`.

### Dependencies

`clock` (shipped) + stdlib: `crypto/rand`, `encoding/hex`, `encoding/binary`,
`database/sql/driver`, `time`, `errors`, `fmt`. No third-party.

---

## Package `ctxkey`

A generic typed key whose **identity** — not its name — is the collision-free context key.

```go
package ctxkey

type Key[T any] struct {
	id   *keyID // distinct unexported sentinel per New() → the real context key
	name string // for Name()/panic messages only
}

func New[T any](name string) Key[T]

func (k Key[T]) With(ctx context.Context, v T) context.Context // context.WithValue(ctx, k.id, v)
func (k Key[T]) From(ctx context.Context) (T, bool)            // typed read, no call-site assertion
func (k Key[T]) MustFrom(ctx context.Context) T               // panics "ctxkey: <name> not in context"
func (k Key[T]) Name() string
```

- **Collision-free by construction:** each `New` allocates a fresh unexported `*keyID` used as
  the context key, so two keys named `"user"` in different packages never alias. Using an
  unexported pointer type as the key also sidesteps `staticcheck SA1029` ("do not use basic
  types as context keys"). No global registry, no reflection.
- `From` on a missing key returns the zero value and `false`. `MustFrom` panics with the key
  name for values that must be present.
- **The logger bridge stays out of `ctxkey`.** It cannot import `logger` (a higher layer).
  Adapting `Key[T].From` into a `logger.ContextExtractor` (`func(ctx) (slog.Attr, bool)`) is a
  one-liner that lives in app wiring or `logger`, keeping `ctxkey` dependency-pure.

### Dependencies

`context` + generics only. Zero forge dependencies.

---

## Testing (black-box)

`package id_test` and `package ctxkey_test`.

**`id`:**
- Round-trip `Parse(x.String()) == x` for all three schemes; `Value`↔`Scan`,
  `MarshalText`↔`UnmarshalText`, and JSON round-trips (JSON is free via `TextMarshaler`).
- `Time()` matches an injected mock clock (`WithClock` + `clock.Mock`).
- **Sortability:** a sequence generated across advancing mock time has string order equal to
  generation order, all three schemes.
- **Monotonic:** at a fixed ms, `Generator` with `WithMonotonic()` yields strictly increasing,
  duplicate-free IDs.
- **KAT vectors:** fixed timestamp + fixed random bytes → exact expected canonical string
  (locks the encoding); UUIDv7 version/variant bits asserted.
- Case-insensitive parse (Crockford `I`/`L`/`O` mapping); `StringLower`/`StringUpper`
  correctness and round-trip.
- `Scan` accepts `string` and `[]byte`, rejects malformed with `ErrMalformed`.
- **Allocation invariants asserted** via `testing.AllocsPerRun`: target **0 allocs** for
  generation, **1** for `String()`. (Honest caveat: if `crypto/rand.Read` forces the stack
  buffer to escape, generation may measure 1 alloc; the implementation optimizes toward 0 and
  treats it as a measured target, not an absolute guarantee.)

**`ctxkey`:**
- `With` then `From` returns the value and `true`; `From` on an empty context returns
  `false`; `MustFrom` returns the value when present and panics when absent.
- **Collision-freedom:** key A cannot read key B's value even with identical names.
- `Name()` returns the declared name.

## Benchmarks

Each package ships `bench_test.go`; every benchmark calls `b.ReportAllocs()`.

**`id`:**
- Generation: `BenchmarkNewUUID`, `BenchmarkNewULID`, `BenchmarkNewShort`,
  `BenchmarkGenerator_ULID` (non-monotonic), `BenchmarkGenerator_Monotonic`.
- Encoding/decoding: `BenchmarkUUID_String`, `BenchmarkULID_String`, `BenchmarkShort_String`,
  `BenchmarkParseUUID`, `BenchmarkParseULID`, `BenchmarkParseShort`.
- DB path: `BenchmarkUUID_Value`, `BenchmarkUUID_Scan`.
- Contention: `b.RunParallel` variants for the lock-free free functions vs the mutex-guarded
  monotonic `Generator`, substantiating the "default = fastest" claim.

**`ctxkey`:** `BenchmarkKey_With`, `BenchmarkKey_From`, `BenchmarkKey_MustFrom`.

## File layout

```
id/
  doc.go
  id.go          // Crockford codec (internal), errors, shared timestamp helpers,
                 //   package-default Generator, free functions
  uuid.go        // UUID type + methods + ParseUUID
  ulid.go        // ULID type + methods + ParseULID
  short.go       // Short type + methods + ParseShort
  generator.go   // Generator, Option, WithClock, WithMonotonic
  *_test.go      // black-box tests
  bench_test.go
ctxkey/
  doc.go
  ctxkey.go
  ctxkey_test.go
  bench_test.go
```

## Out of scope

- pgx binary-path type registration for `UUID` (belongs in the `postgres` data package).
- The `logger.ContextExtractor` adapter for `ctxkey` (belongs in app wiring / `logger`).
- Any downstream consumer: `requestid`, `session`, `tenant`, `authmw`, etc.
- Type-prefixed / TypeID-style IDs (explicitly not chosen; the three schemes above are final).
- Configurable `Short` length (fixed 16 chars / 80 bits by decision — fixed width is required
  for lexicographic sortability and a fixed-size value type).
```