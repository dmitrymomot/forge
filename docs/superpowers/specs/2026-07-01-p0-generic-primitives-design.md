# Design: P0 generic-leaf primitives (7 packages)

- **Date:** 2026-07-01
- **Status:** Draft for review
- **Scope:** Seven flat top-level packages from the P0 layer of
  `docs/maximal-package-set.md` — `slicex`, `ptr`, `mapx`, `set`, `encoding`, `enum`,
  `stringsx`. All are tiny, stdlib-only, mostly-generic **leaf** utilities with zero forge
  dependencies. The single new *internal* edge is `id → encoding`, and it is **conditional**
  (see the encoding/id gate). This spec builds only these primitives; it does not build
  downstream consumers (`nullx`, `bind`, `form`, `envconfig`, etc.).

## Overview

These are the cheapest, most parallelizable slice of the P0 base: pure generic value-types
and free functions that fill gaps stdlib leaves open. They carry no forge dependencies (they
are the leaves of the dependency DAG), so they can be built and reviewed independently and in
parallel. Each does one small job, returns plain values and `errors.Is`-matchable sentinels,
and gets out of the way.

Two design decisions were settled during brainstorming and are baked into this spec:

1. **Full sketches, not lean.** `ptr` ships the `Optional[T]` type (not just the trivial
   pointer helpers), and `encoding` is intended to become the *shared* Crockford codec that
   `id` consumes.
2. **The `id → encoding` refactor is gated.** `id` already contains a private, alloc-tuned
   Crockford base32 encoder. We build `encoding` as a general-purpose codec and *attempt* to
   switch `id` onto it, but only keep that switch if it passes two hard checks (byte-for-byte
   identical output and no allocation regression). If either check fails, `id` keeps its
   private encoder and `encoding` ships standalone. Details in the encoding section.

The `enum` value-domain type is named **`enum.Values[T]`** (not `enum.Set[T]`) to avoid
confusion with the mutable-collection `set.Set[T]` shipping in the same batch.

## Design DNA (applies to every package)

Each package follows the conventions established by the shipped packages:

- **Flat layout.** One top-level directory per package: `doc.go` (package doc) plus
  concern-split `*.go` files. No nested subpackages.
- **Stdlib-only, zero forge deps.** These are leaves. The one exception is the conditional
  `id → encoding` internal edge. No new external (non-stdlib) module dependencies —
  `math/big` (used by Base62) is stdlib.
- **Free functions + generic value types.** No constructors-with-options where a free
  function will do, no builder pattern.
- **Sentinel errors.** `errors.New`-based, `errors.Is`-friendly, single-line (per the
  structured-logging convention). Each package that can fail exports named sentinels.
- **Go 1.26 idioms.** `iter.Seq`/`iter.Seq2` for iteration, stdlib `slices`/`maps`/`cmp`
  for what they already cover (we fill gaps only), and JSON `omitzero`/`IsZero()` for
  optional-field omission.
- **Black-box tests.** Tests live in an external `<pkg>_test` package and exercise only the
  exported API; white-box only where an unexported invariant genuinely must be asserted.
  Table-driven, with `bench_test.go` where a hot path exists.
- **`just check` green.** `just fmt` (which runs `betteralign -apply`) before commit;
  `go vet`, `golangci-lint`, `nilaway`, `betteralign`, and `modernize` all clean.

---

## 1. `slicex` — generic slice gap-fillers

Generic helpers that stdlib `slices` does not provide. Fills gaps **only** — does not
re-implement Sort/SortFunc/Contains/Index/Equal/Reverse/Compact, which are stdlib.

**No aliasing of stdlib (framework-wide rule, stated in `doc.go`).** `slicex` neither
re-implements nor re-exports stdlib `slices` functions — consumers import `slices` directly
alongside `slicex`. Reasons: generic functions can't be cheaply aliased (`var Sort = slices.Sort`
is illegal; each "alias" would be a hand-written generic wrapper with copied constraints), such
wrappers drift as stdlib grows new helpers/variants, and two names for one function (`slices.Sort`
vs `slicex.Sort`) is a two-sources-of-truth footgun. stdlib is the most stable dependency possible
and needs no wrapper; the one-extra-import cost is idiomatic Go and free. The same
**gap-fill-only, never-re-export-stdlib** rule applies to `mapx` (vs `maps`) and `set`.

```go
func Map[T, U any](s []T, fn func(T) U) []U
func Filter[T any](s []T, pred func(T) bool) []T
func Reduce[T, U any](s []T, init U, fn func(acc U, v T) U) U
func GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T
func KeyBy[T any, K comparable](s []T, key func(T) K) map[K]T   // last value wins on dup key
func Unique[T comparable](s []T) []T                            // order-preserving
func Flatten[T any](s [][]T) []T
func Chunk[T any](s []T, n int) [][]T                           // materialized; panics if n < 1
```

**Notes / edge cases:**

- `Unique` preserves first-seen order (unlike `slices.Compact`, which requires sorted input
  and removes only *adjacent* duplicates).
- `Chunk` returns a materialized `[][]T`; stdlib `slices.Chunk` returns an `iter.Seq[[]T]`.
  We provide the materialized form because it is the common need. It **panics when `n < 1`**,
  matching stdlib `slices.Chunk`'s contract. The final chunk may be shorter.
- `Map`/`Filter`/`Reduce` return non-nil empty (`[]U{}`) for empty non-nil input, and `nil`
  for `nil` input, so round-tripping preserves nilness. (Assert this in tests.)
- **Deps:** `slices` (only where it helps internally). No `iter` needed — slice in, slice out.

## 2. `ptr` — pointer helpers + `Optional[T]`

Trivial pointer helpers for optional struct fields / JSON `omitempty` / SQL nullables, plus a
small `Optional[T]` type for "was this field provided?" semantics.

**No `To` helper.** Go 1.26's built-in `new(expr)` returns a pointer to a copy of an
expression (`new(42)` → `*int`), so a pointer-to-literal helper would just wrap a language
builtin — and the repo's `modernize` lint rejects it. `ptr` therefore does **not** ship `To`;
callers use `new(v)`.

```go
func From[T any](p *T) T                 // zero value if p == nil
func FromOr[T any](p *T, def T) T        // def if p == nil
func Equal[T comparable](a, b *T) bool   // both nil => true; one nil => false; else *a == *b

type Optional[T any] struct { /* value T; defined bool — unexported */ }

func Some[T any](v T) Optional[T]
func None[T any]() Optional[T]

func (o Optional[T]) Get() (T, bool)     // (value, defined)
func (o Optional[T]) IsDefined() bool
func (o Optional[T]) OrElse(def T) T
func (o Optional[T]) IsZero() bool       // == !defined; enables json:",omitzero"
func (o Optional[T]) MarshalJSON() ([]byte, error)
func (o *Optional[T]) UnmarshalJSON(b []byte) error
```

**Semantics:**

- `Optional[T]` is **two-state**: `defined` or not. `UnmarshalJSON` is only called by
  `encoding/json` when the key is *present* in the payload — so a present key (even
  `"field": null`) sets `defined = true`, and an absent key leaves `defined = false`. That is
  the "was it provided?" signal for PATCH.
- `MarshalJSON` emits the encoded value when defined, and `null` when not. To **omit** an
  undefined `Optional` from output entirely, the caller tags the field
  `json:"field,omitzero"` — `IsZero()` returns `!defined`, and Go 1.26's `omitzero` honors it.
- **PATCH's three-way absent / null / value** is achieved by *composition*, not a third state:
  `Optional[*T]` distinguishes absent (`!defined`) from explicit null (`defined`, inner `*T`
  is nil) from a value. `Optional[nullx.Null[T]]` will compose the same way once `nullx`
  lands. We deliberately do **not** build a 3-state Optional.
- **Deps:** `encoding/json`. Pure generics otherwise.

## 3. `mapx` — generic map gap-fillers + `Ordered[K,V]`

Generic helpers complementing stdlib `maps` (does not duplicate Clone/Keys/Values/Equal/Copy),
plus an insertion-ordered map for stable JSON.

```go
func Merge[K comparable, V any](maps ...map[K]V) map[K]V          // later maps win
func MapValues[K comparable, V, U any](m map[K]V, fn func(V) U) map[K]U
func Invert[K, V comparable](m map[K]V) map[V]K                   // last key wins on dup value
func Filter[K comparable, V any](m map[K]V, pred func(K, V) bool) map[K]V
func Entries[K comparable, V any](m map[K]V) []Entry[K, V]        // unordered
func FromEntries[K comparable, V any](es []Entry[K, V]) map[K]V

type Entry[K comparable, V any] struct { Key K; Value V }

// Insertion-ordered map with order-preserving JSON.
type Ordered[K comparable, V any] struct { /* keys []K; m map[K]V — unexported */ }

func NewOrdered[K comparable, V any]() *Ordered[K, V]
func (o *Ordered[K, V]) Set(k K, v V)
func (o *Ordered[K, V]) Get(k K) (V, bool)
func (o *Ordered[K, V]) Delete(k K)
func (o *Ordered[K, V]) Len() int
func (o *Ordered[K, V]) Keys() []K                    // insertion order
func (o *Ordered[K, V]) All() iter.Seq2[K, V]         // insertion order
func (o *Ordered[K, V]) MarshalJSON() ([]byte, error)   // object in insertion order
func (o *Ordered[K, V]) UnmarshalJSON(b []byte) error   // preserves source key order
```

**Notes / edge cases:**

- `Ordered.Set` on an existing key updates the value **without** changing its position;
  `Delete` removes it from the key order. Re-`Set` after `Delete` appends at the end.
- `Ordered.UnmarshalJSON` preserves source key order by reading tokens with a
  `*json.Decoder` (Token stream) rather than decoding into a `map` (which loses order).
  Requires `K` to be a string-like key for JSON object semantics; non-string `K` marshals as
  a JSON object only when the key encodes to a string (same constraint stdlib maps have).
  This is the one non-trivial piece (~80 LOC); covered by round-trip + order-stability tests.
- **Deps:** `maps`, `iter`, `encoding/json`.

## 4. `set` — `Set[T comparable]`

A generic set with the algebra stdlib does not provide.

```go
type Set[T comparable] struct { /* m map[T]struct{} — unexported */ }

func New[T comparable](items ...T) Set[T]

func (s *Set[T]) Add(items ...T)
func (s *Set[T]) Remove(items ...T)
func (s Set[T]) Contains(v T) bool
func (s Set[T]) Len() int
func (s Set[T]) IsEmpty() bool
func (s Set[T]) Union(other Set[T]) Set[T]
func (s Set[T]) Intersect(other Set[T]) Set[T]
func (s Set[T]) Diff(other Set[T]) Set[T]       // elements in s not in other
func (s Set[T]) Equal(other Set[T]) bool
func (s Set[T]) Slice() []T                      // undefined order
func (s Set[T]) Sorted(less func(a, b T) bool) []T
func (s Set[T]) All() iter.Seq[T]
```

**Semantics:**

- `Set[T]` is a **value type wrapping a map**. `New` is the idiomatic constructor, but the
  **zero `Set[T]` is usable**: `Add` lazily allocates the backing map, and read/algebra
  methods (`Contains`/`Len`/`Union`/…) treat a nil-map set as empty. Because it wraps a map,
  **copying a non-empty `Set` shares the backing store** — the standard Go map-wrapper caveat,
  documented on the type. Callers who need an independent copy use
  `other.Union(set.New[T]())` (a `Clone()` is omitted for now — YAGNI, add when a consumer
  needs it).
- `Union`/`Intersect`/`Diff` return **new** sets and never mutate the receiver or argument.
- `Sorted` takes an explicit `less` because `T` is only `comparable`, not `cmp.Ordered`.
- **Deps:** `maps`, `slices`, `iter`.

## 5. `encoding` — Base62 + Crockford base32 codecs

Compact, URL-safe, human-typable byte/integer codecs. This is intended to become the shared
Crockford implementation `id` consumes (gated — see below).

```go
// Base62 (0-9A-Za-z), for compact integer/byte encoding.
func EncodeInt(n uint64) string
func DecodeInt(s string) (uint64, error)
func Encode(b []byte) string          // Base62 of an arbitrary byte slice (via math/big)
func Decode(s string) ([]byte, error)

// Crockford base32 (canonical: excludes I, L, O, U; MSB-first bit packing).
func Encode32(b []byte) string
func Decode32(s string) ([]byte, error)   // case-insensitive; aliases I/L -> 1, O -> 0

var ErrInvalidEncoding = errors.New("encoding: invalid input")
```

**Crockford correctness contract (this is what makes the `id` refactor possible):**

- `Encode32` implements **canonical, MSB-first, big-endian, left-padded** Crockford base32.
  For a 16-byte input it produces a 26-character string; for a 10-byte input, 16 characters —
  the exact widths `id`'s ULID and Short types already use.
- `Decode32` is case-insensitive and applies Crockford's decode aliases (`I`,`i`,`L`,`l` → 1;
  `O`,`o` → 0). Invalid characters return `ErrInvalidEncoding`.
- **Deps:** `math/big` (Base62 arbitrary-byte path), `strings`, `errors`. All stdlib.

### The `id → encoding` gate

`id` currently has a private, alloc-tuned Crockford encoder used by its `ULID` and `Short`
types. We attempt to delete it and route `id` through `encoding.Encode32`/`Decode32`. We keep
that switch **only if both hard checks pass**:

1. **Byte-for-byte identical output.** `id`'s existing tests — including the ULID Known-Answer
   Tests (e.g. `01ARZ3NDEK...` decoding to its known millisecond value) and all
   `String()`/`Parse*` round-trips — must pass **unchanged**. `encoding`'s canonical Crockford
   must reproduce `id`'s current strings exactly.
2. **No allocation regression.** `id`'s `alloc_test.go` asserts `String()` allocates `<= 1`.
   After the switch, those assertions and the `bench_test.go` numbers must hold (no new
   allocations, no material slowdown).

**If both pass:** `id` imports `encoding`, its private codec is deleted, and the `id → encoding`
edge is added to the DAG. **If either fails:** revert `id` to its private codec; `encoding`
ships as a standalone general-purpose package with no `id` dependency. Either outcome is a
success — `encoding` is built and correct regardless. The plan captures this as an explicit
verify-or-revert step so the batch never blocks on it.

## 6. `enum` — `Values[T ~string]`

A fixed, declared closed value-set over a named string type. Distinct from `set` (a mutable
runtime collection); `enum` is an immutable value-domain declared once.

```go
type Values[T ~string] struct { /* ordered []T; set map[T]struct{} — unexported */ }

func New[T ~string](vals ...T) Values[T]

func (e Values[T]) Parse(s string) (T, error)   // exact match; ErrInvalidValue otherwise
func (e Values[T]) Valid(v T) bool
func (e Values[T]) Values() []T                 // declared order

var ErrInvalidValue = errors.New("enum: invalid value")
```

**Usage:**

```go
type Status string
const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
)
var Statuses = enum.New(StatusActive, StatusPaused)

s, err := Statuses.Parse(input)  // err == enum.ErrInvalidValue for unknown input
```

**Notes:** `Parse` is exact / case-sensitive (matching a declared value literally). `Values`
returns declared order. Duplicate declared values are de-duplicated (documented). **Deps:**
stdlib only, data-only, no I/O.

## 7. `stringsx` — formatting for *trusted* strings

String-shaping helpers stdlib lacks. For **trusted** strings — untrusted input is `sanitize`'s
job (a later package, not in this batch).

```go
func ToSnake(s string) string   // "UserID" -> "user_id"
func ToCamel(s string) string   // "user_id" -> "userID" (lowerCamel)
func ToKebab(s string) string   // "UserID" -> "user-id"
func Truncate(s string, n int) string        // hard cut to n runes (rune-safe)
func Ellipsis(s string, n int) string         // cut to n runes, append "…" if truncated
func TruncateWords(s string, n int) string    // first n whitespace-separated words
func Mask(s string, keep int) string          // keep last `keep` runes, star the rest
func Pluralize(word string, n int) string     // BASIC English only
```

**Notes / scope guards:**

- All length operations are **rune-safe** (count runes, never split a multi-byte rune).
- `Ellipsis` counts the ellipsis toward nothing special — it appends `"…"` only when the
  input actually exceeded `n` runes; `n <= 0` returns empty (documented).
- `Mask(s, keep)`: if `keep >= len(runes)`, returns all-stars of the original rune length (no
  leakage); `keep <= 0` masks everything. Star count equals the masked rune count.
- `Pluralize` is deliberately **naive**: append `s`; `es` for words ending in s/x/z/ch/sh;
  `y → ies` (consonant + y). It is documented as a best-effort helper, **not** a linguistics
  engine (no irregular plurals). Returns the singular unchanged when `n == 1`.
  **Locale-aware / rule-based pluralization is out of scope here** — that will be owned by the
  future `i18n` package (multi-language + custom CLDR-style plural rules). `stringsx.Pluralize`
  is only the naive English helper for trusted, developer-facing strings; `doc.go` cross-references
  `i18n` so consumers don't reach for it when they need real localization.
- **Deps:** `strings`, `unicode`, `unicode/utf8`.

---

## Cross-cutting concerns

### Dependencies

- **External (go.mod):** none added. Everything is stdlib (`math/big` for Base62 is stdlib).
- **Internal edges:** only the conditional `id → encoding`. The other six packages are pure
  leaves with no forge imports.

### Build approach

Built via subagent-driven-development. The six independent leaves (`slicex`, `ptr`, `mapx`,
`set`, `enum`, `stringsx`) parallelize freely — no shared state, no ordering. `encoding` plus
the `id` integration is one sequenced unit ending in the KAT+alloc verify-or-revert gate.
Per-package review, then a final opus review over the whole batch.

### Testing

- Black-box table-driven tests per package (external `<pkg>_test` package).
- **`encoding`:** round-trip properties (`Decode(Encode(b)) == b`) across random and edge
  inputs (empty, single byte, all-zero, max), invalid-char rejection, decode-alias coverage,
  and canonical-vector checks. After the `id` switch (if kept), `id`'s full existing suite —
  KATs, round-trips, `alloc_test.go`, `bench_test.go` — must stay green.
- **`ptr.Optional` / `mapx.Ordered`:** JSON round-trip and absent-vs-present / order-stability
  tests, including `omitzero` omission for `Optional`.
- **`set` / `enum`:** algebra correctness, determinism, and the copy-aliasing caveat asserted
  for `set`.
- **`stringsx`:** rune-safety on multi-byte input, case-conversion tables, mask boundaries.

### Top risks

1. **`encoding` ↔ `id` byte-compat and allocation** — the one real risk. Fully contained by
   the verify-or-revert gate with a documented fallback (`id` keeps its private codec). No
   other package is affected by the outcome.
2. **`mapx.Ordered` JSON order preservation** — moderate complexity in `UnmarshalJSON`
   (token-stream decode). Covered by order-stability tests.
3. **`set` copy-aliasing** — inherent to map-wrapping value types; mitigated by documentation
   and a test that demonstrates the shared-backing behavior so it is explicit, not surprising.

## Out of scope (deliberate)

- `nullx`, `bind`, `form`, `envconfig`, `structfields`, `typeconv`, `validate` and every other
  P0/P3 consumer — later phases.
- A 3-state `Optional` (composition via `Optional[*T]` covers it).
- Irregular-plural / i18n-aware pluralization (`stringsx.Pluralize` stays naive).
- `set.Clone` and other convenience methods until a consumer needs them (YAGNI).
