# P0 generic-leaf primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build seven stdlib-only P0 leaf packages — `slicex`, `ptr`, `mapx`, `set`, `encoding`, `enum`, `stringsx` — that fill gaps the standard library leaves open, plus a gated refactor of `id` onto the new shared `encoding` codec.

**Architecture:** Each package is a flat top-level directory of generic free functions and small value types with zero forge dependencies (leaves). The only internal edge is `id → encoding`, added **only if** the shared Crockford codec reproduces `id`'s output byte-for-byte and does not regress its allocation bounds; otherwise `id` keeps its private codec and `encoding` ships standalone.

**Tech Stack:** Go 1.26, stdlib only (`slices`, `maps`, `iter`, `cmp`, `encoding/json`, `math/big`, `strings`, `unicode`, `errors`). Tests use `testify` (black-box, `package X_test`).

**Spec:** `docs/superpowers/specs/2026-07-01-p0-generic-primitives-design.md`

## Global Constraints

- **Module:** `github.com/dmitrymomot/forge`; Go **1.26**.
- **Flat layout:** files live directly in each package dir; no nested folders.
- **stdlib-only production code**; zero forge deps except the gated `id → encoding` edge.
- **No stdlib aliasing:** `slicex`/`mapx`/`set` neither re-implement nor re-export stdlib `slices`/`maps` functions. State the rationale in each `doc.go`.
- **Options, not builders** (not needed in this batch — no constructor takes options).
- **Black-box tests only** — `package <name>_test`; white-box only to assert unexported invariants.
- **`errors.Is` sentinels** for error matching.
- **No Claude attribution** in commit messages.
- Run **`just fmt`** before every commit (applies `betteralign` struct reordering + goimports); **`just check`** = fmt + lint + test must be green. Targeted runs use `go test ./<pkg>/...`.
- Tests are table-driven; each package ships `doc.go` + concern-split `*.go` + `<name>_test.go` (+ `bench_test.go` where a hot path exists).

---

### Task 1: `slicex` package

**Files:**
- Create: `slicex/slicex.go`
- Create: `slicex/doc.go`
- Test: `slicex/slicex_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces:
  - `func Map[T, U any](s []T, fn func(T) U) []U`
  - `func Filter[T any](s []T, pred func(T) bool) []T`
  - `func Reduce[T, U any](s []T, init U, fn func(acc U, v T) U) U`
  - `func GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T`
  - `func KeyBy[T any, K comparable](s []T, key func(T) K) map[K]T`
  - `func Unique[T comparable](s []T) []T`
  - `func Flatten[T any](s [][]T) []T`
  - `func Chunk[T any](s []T, n int) [][]T`

- [ ] **Step 1: Write the failing tests**

Create `slicex/slicex_test.go`:

```go
package slicex_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/slicex"
)

func TestMap(t *testing.T) {
	got := slicex.Map([]int{1, 2, 3}, func(v int) string { return string(rune('a' + v)) })
	assert.Equal(t, []string{"b", "c", "d"}, got)

	assert.Nil(t, slicex.Map(nil, func(v int) int { return v }), "nil in -> nil out")
	assert.Equal(t, []int{}, slicex.Map([]int{}, func(v int) int { return v }), "empty non-nil -> empty non-nil")
}

func TestFilter(t *testing.T) {
	got := slicex.Filter([]int{1, 2, 3, 4}, func(v int) bool { return v%2 == 0 })
	assert.Equal(t, []int{2, 4}, got)
	assert.Nil(t, slicex.Filter(nil, func(int) bool { return true }))
}

func TestReduce(t *testing.T) {
	sum := slicex.Reduce([]int{1, 2, 3}, 0, func(acc, v int) int { return acc + v })
	assert.Equal(t, 6, sum)
	assert.Equal(t, 10, slicex.Reduce(nil, 10, func(acc, v int) int { return acc + v }), "empty -> init")
}

func TestGroupBy(t *testing.T) {
	got := slicex.GroupBy([]int{1, 2, 3, 4}, func(v int) int { return v % 2 })
	assert.Equal(t, map[int][]int{0: {2, 4}, 1: {1, 3}}, got)
}

func TestKeyBy(t *testing.T) {
	type u struct {
		id   int
		name string
	}
	got := slicex.KeyBy([]u{{1, "a"}, {2, "b"}, {1, "c"}}, func(x u) int { return x.id })
	assert.Equal(t, u{1, "c"}, got[1], "last value wins on duplicate key")
	assert.Equal(t, u{2, "b"}, got[2])
}

func TestUnique(t *testing.T) {
	assert.Equal(t, []int{3, 1, 2}, slicex.Unique([]int{3, 1, 3, 2, 1}), "first-seen order preserved")
	assert.Nil(t, slicex.Unique[int](nil))
}

func TestFlatten(t *testing.T) {
	assert.Equal(t, []int{1, 2, 3, 4}, slicex.Flatten([][]int{{1, 2}, {}, {3, 4}}))
	assert.Nil(t, slicex.Flatten[int](nil))
}

func TestChunk(t *testing.T) {
	assert.Equal(t, [][]int{{1, 2}, {3, 4}, {5}}, slicex.Chunk([]int{1, 2, 3, 4, 5}, 2))
	assert.Nil(t, slicex.Chunk[int](nil, 2))
}

func TestChunk_PanicsOnNonPositiveN(t *testing.T) {
	require.Panics(t, func() { slicex.Chunk([]int{1}, 0) })
	require.Panics(t, func() { slicex.Chunk([]int{1}, -1) })
}

func TestChunk_NoAliasingAppend(t *testing.T) {
	src := []int{1, 2, 3, 4}
	chunks := slicex.Chunk(src, 2)
	chunks[0] = append(chunks[0], 99) // must not overwrite src[2]
	assert.Equal(t, 3, src[2], "chunk append must not spill into the source slice")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./slicex/...`
Expected: FAIL — `package github.com/dmitrymomot/forge/slicex is not in std` / undefined functions.

- [ ] **Step 3: Write the implementation**

Create `slicex/slicex.go`:

```go
package slicex

// Map returns a new slice with fn applied to each element of s.
// A nil input yields a nil result; an empty non-nil input yields an empty
// non-nil result (nilness is preserved).
func Map[T, U any](s []T, fn func(T) U) []U {
	if s == nil {
		return nil
	}
	out := make([]U, len(s))
	for i, v := range s {
		out[i] = fn(v)
	}
	return out
}

// Filter returns a new slice of the elements of s for which pred returns true.
func Filter[T any](s []T, pred func(T) bool) []T {
	if s == nil {
		return nil
	}
	out := make([]T, 0, len(s))
	for _, v := range s {
		if pred(v) {
			out = append(out, v)
		}
	}
	return out
}

// Reduce folds s left-to-right into a single value starting from init.
func Reduce[T, U any](s []T, init U, fn func(acc U, v T) U) U {
	acc := init
	for _, v := range s {
		acc = fn(acc, v)
	}
	return acc
}

// GroupBy buckets the elements of s by key, preserving per-bucket order.
func GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T {
	out := make(map[K][]T)
	for _, v := range s {
		k := key(v)
		out[k] = append(out[k], v)
	}
	return out
}

// KeyBy indexes s by key. On duplicate keys the last element wins.
func KeyBy[T any, K comparable](s []T, key func(T) K) map[K]T {
	out := make(map[K]T, len(s))
	for _, v := range s {
		out[key(v)] = v
	}
	return out
}

// Unique returns the elements of s with duplicates removed, preserving
// first-seen order (unlike slices.Compact, which needs sorted input).
func Unique[T comparable](s []T) []T {
	if s == nil {
		return nil
	}
	seen := make(map[T]struct{}, len(s))
	out := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Flatten concatenates the sub-slices of s into one slice.
func Flatten[T any](s [][]T) []T {
	if s == nil {
		return nil
	}
	n := 0
	for _, sub := range s {
		n += len(sub)
	}
	out := make([]T, 0, n)
	for _, sub := range s {
		out = append(out, sub...)
	}
	return out
}

// Chunk splits s into consecutive slices of at most n elements. The final
// chunk may be shorter. Chunk panics if n < 1, matching slices.Chunk. Unlike
// slices.Chunk it returns a materialized [][]T rather than an iterator; each
// chunk has capacity clamped so appending to it cannot overwrite s.
func Chunk[T any](s []T, n int) [][]T {
	if n < 1 {
		panic("slicex: Chunk called with n < 1")
	}
	if s == nil {
		return nil
	}
	out := make([][]T, 0, (len(s)+n-1)/n)
	for i := 0; i < len(s); i += n {
		end := min(i+n, len(s))
		out = append(out, s[i:end:end])
	}
	return out
}
```

Create `slicex/doc.go`:

```go
// Package slicex provides generic slice helpers that the standard library
// slices package does not: Map, Filter, Reduce, GroupBy, KeyBy, Unique,
// Flatten, and a materialized Chunk.
//
// slicex is a gap-filler, not a superset. It deliberately does NOT re-implement
// or re-export functions that already live in stdlib slices (Sort, SortFunc,
// Contains, Index, Equal, Reverse, Compact, ...). Import "slices" directly
// alongside slicex. Aliasing stdlib is avoided on purpose: generic functions
// cannot be cheaply aliased (var Sort = slices.Sort is illegal; each alias
// would be a hand-written generic wrapper), such wrappers drift as stdlib grows
// new helpers, and two names for one function is a two-sources-of-truth footgun.
package slicex
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./slicex/...`
Expected: PASS (all tests).

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt
go test ./slicex/... && go vet ./slicex/...
git add slicex/
git commit -m "feat(slicex): generic slice gap-fillers (Map/Filter/Reduce/GroupBy/KeyBy/Unique/Flatten/Chunk)"
```

---

### Task 2: `ptr` package

**Files:**
- Create: `ptr/ptr.go`
- Create: `ptr/optional.go`
- Create: `ptr/doc.go`
- Test: `ptr/ptr_test.go`
- Test: `ptr/optional_test.go`

**Interfaces:**
- Consumes: stdlib `encoding/json`.
- Produces (note: **no `To`** — Go 1.26's `new(expr)` builtin supersedes it; the repo `modernize` lint rejects the wrapper):
  - `func From[T any](p *T) T`
  - `func FromOr[T any](p *T, def T) T`
  - `func Equal[T comparable](a, b *T) bool`
  - `type Optional[T any]` with `Some[T](v) Optional[T]`, `None[T]() Optional[T]`, `(Optional[T]).Get() (T, bool)`, `.IsDefined() bool`, `.OrElse(def T) T`, `.IsZero() bool`, `MarshalJSON`, `UnmarshalJSON`.

- [ ] **Step 1: Write the failing tests**

Create `ptr/ptr_test.go`:

```go
package ptr_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/ptr"
)

func TestFrom(t *testing.T) {
	assert.Equal(t, 7, ptr.From(new(7)))
	assert.Equal(t, 0, ptr.From[int](nil), "nil -> zero value")
}

func TestFromOr(t *testing.T) {
	assert.Equal(t, 7, ptr.FromOr(new(7), 99))
	assert.Equal(t, 99, ptr.FromOr[int](nil, 99))
}

func TestEqual(t *testing.T) {
	assert.True(t, ptr.Equal[int](nil, nil), "both nil equal")
	assert.False(t, ptr.Equal(new(1), nil), "one nil unequal")
	assert.False(t, ptr.Equal[int](nil, new(1)))
	assert.True(t, ptr.Equal(new(5), new(5)))
	assert.False(t, ptr.Equal(new(5), new(6)))
}
```

Create `ptr/optional_test.go`:

```go
package ptr_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ptr"
)

func TestOptional_SomeNone(t *testing.T) {
	s := ptr.Some(10)
	v, ok := s.Get()
	assert.True(t, ok)
	assert.Equal(t, 10, v)
	assert.True(t, s.IsDefined())
	assert.False(t, s.IsZero())
	assert.Equal(t, 10, s.OrElse(99))

	n := ptr.None[int]()
	_, ok = n.Get()
	assert.False(t, ok)
	assert.False(t, n.IsDefined())
	assert.True(t, n.IsZero())
	assert.Equal(t, 99, n.OrElse(99))
}

func TestOptional_Marshal(t *testing.T) {
	b, err := json.Marshal(ptr.Some("x"))
	require.NoError(t, err)
	assert.JSONEq(t, `"x"`, string(b))

	b, err = json.Marshal(ptr.None[string]())
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))
}

type patch struct {
	Name ptr.Optional[string] `json:"name,omitzero"`
}

func TestOptional_OmitzeroAndAbsentVsNull(t *testing.T) {
	// None omitted entirely on output via omitzero.
	b, err := json.Marshal(patch{Name: ptr.None[string]()})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(b))

	// Some serialized.
	b, err = json.Marshal(patch{Name: ptr.Some("hi")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"hi"}`, string(b))

	// Absent key -> not defined.
	var p patch
	require.NoError(t, json.Unmarshal([]byte(`{}`), &p))
	assert.False(t, p.Name.IsDefined(), "absent key => not defined")

	// Explicit null -> defined, zero value.
	p = patch{}
	require.NoError(t, json.Unmarshal([]byte(`{"name":null}`), &p))
	assert.True(t, p.Name.IsDefined(), "present null => defined")
	v, _ := p.Name.Get()
	assert.Equal(t, "", v)

	// Present value -> defined.
	p = patch{}
	require.NoError(t, json.Unmarshal([]byte(`{"name":"set"}`), &p))
	assert.True(t, p.Name.IsDefined())
	v, _ = p.Name.Get()
	assert.Equal(t, "set", v)
}

func TestOptional_PointerNullVsAbsent(t *testing.T) {
	type patchP struct {
		Bio ptr.Optional[*string] `json:"bio,omitzero"`
	}
	// Explicit null on a pointer T: defined, inner pointer nil (clear the field).
	var p patchP
	require.NoError(t, json.Unmarshal([]byte(`{"bio":null}`), &p))
	assert.True(t, p.Bio.IsDefined())
	inner, _ := p.Bio.Get()
	assert.Nil(t, inner, "explicit null => defined with nil pointer")

	// Absent: not defined (don't touch).
	p = patchP{}
	require.NoError(t, json.Unmarshal([]byte(`{}`), &p))
	assert.False(t, p.Bio.IsDefined())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ptr/...`
Expected: FAIL — undefined `ptr.*`.

- [ ] **Step 3: Write the implementation**

Create `ptr/ptr.go`:

```go
package ptr

// (No To helper: Go 1.26's new(expr) builtin returns a pointer to a copy of an
// expression, e.g. new(42), so a pointer-to-literal helper is redundant.)

// From dereferences p, returning the zero value of T when p is nil.
func From[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// FromOr dereferences p, returning def when p is nil.
func FromOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// Equal reports whether a and b point to equal values. Two nil pointers are
// equal; a nil and a non-nil pointer are not.
func Equal[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
```

Create `ptr/optional.go`:

```go
package ptr

import "encoding/json"

// Optional is a two-state value: either defined (carrying a T) or not. It is
// the "was this field provided?" signal for JSON PATCH bodies: UnmarshalJSON is
// only invoked by encoding/json when the key is present, so an absent key
// leaves the Optional undefined while a present key (even null) marks it
// defined.
//
// The three-way absent / null / value distinction is obtained by composition
// rather than a third state: Optional[*T] gives absent (!IsDefined), explicit
// null (defined, inner *T nil), and value.
type Optional[T any] struct {
	value   T
	defined bool
}

// Some returns a defined Optional carrying v.
func Some[T any](v T) Optional[T] { return Optional[T]{value: v, defined: true} }

// None returns an undefined Optional.
func None[T any]() Optional[T] { return Optional[T]{} }

// Get returns the value and whether the Optional is defined.
func (o Optional[T]) Get() (T, bool) { return o.value, o.defined }

// IsDefined reports whether a value is present.
func (o Optional[T]) IsDefined() bool { return o.defined }

// OrElse returns the value when defined, otherwise def.
func (o Optional[T]) OrElse(def T) T {
	if o.defined {
		return o.value
	}
	return def
}

// IsZero reports whether the Optional is undefined. It enables the
// encoding/json ",omitzero" tag to omit an undefined Optional from output.
func (o Optional[T]) IsZero() bool { return !o.defined }

// MarshalJSON emits the encoded value when defined, otherwise null.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if !o.defined {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// UnmarshalJSON marks the Optional defined (the key was present) and decodes
// the value. A JSON null leaves the value as the zero value of T (for pointer T
// that is a nil pointer, i.e. an explicit clear).
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.defined = true
	if string(b) == "null" {
		var zero T
		o.value = zero
		return nil
	}
	return json.Unmarshal(b, &o.value)
}
```

Create `ptr/doc.go`:

```go
// Package ptr provides generic pointer helpers (From, FromOr, Equal) for
// optional struct fields, JSON omitempty, and SQL nullables, plus Optional[T],
// a two-state "provided?" wrapper for JSON PATCH semantics. A pointer to a
// literal is the Go 1.26 new(expr) builtin, so ptr does not wrap it.
package ptr
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./ptr/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt
go test ./ptr/... && go vet ./ptr/...
git add ptr/
git commit -m "feat(ptr): pointer helpers + Optional[T] with PATCH-friendly JSON"
```

---

### Task 3: `mapx` package

**Files:**
- Create: `mapx/mapx.go`
- Create: `mapx/ordered.go`
- Create: `mapx/doc.go`
- Test: `mapx/mapx_test.go`
- Test: `mapx/ordered_test.go`

**Interfaces:**
- Consumes: stdlib `encoding/json`, `iter`.
- Produces:
  - `func Merge[K comparable, V any](maps ...map[K]V) map[K]V`
  - `func MapValues[K comparable, V, U any](m map[K]V, fn func(V) U) map[K]U`
  - `func Invert[K, V comparable](m map[K]V) map[V]K`
  - `func Filter[K comparable, V any](m map[K]V, pred func(K, V) bool) map[K]V`
  - `type Entry[K comparable, V any] struct { Key K; Value V }`
  - `func Entries[K comparable, V any](m map[K]V) []Entry[K, V]`
  - `func FromEntries[K comparable, V any](es []Entry[K, V]) map[K]V`
  - `type Ordered[K comparable, V any]` with `NewOrdered`, `Set`, `Get`, `Delete`, `Len`, `Keys`, `All`, `MarshalJSON`, `UnmarshalJSON`.

- [ ] **Step 1: Write the failing tests**

Create `mapx/mapx_test.go`:

```go
package mapx_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/mapx"
)

func TestMerge(t *testing.T) {
	got := mapx.Merge(map[string]int{"a": 1, "b": 2}, map[string]int{"b": 20, "c": 3})
	assert.Equal(t, map[string]int{"a": 1, "b": 20, "c": 3}, got, "later maps win")
}

func TestMapValues(t *testing.T) {
	got := mapx.MapValues(map[string]int{"a": 1, "b": 2}, func(v int) int { return v * 10 })
	assert.Equal(t, map[string]int{"a": 10, "b": 20}, got)
}

func TestInvert(t *testing.T) {
	got := mapx.Invert(map[string]int{"a": 1, "b": 2})
	assert.Equal(t, map[int]string{1: "a", 2: "b"}, got)
}

func TestFilter(t *testing.T) {
	got := mapx.Filter(map[string]int{"a": 1, "b": 2, "c": 3}, func(_ string, v int) bool { return v%2 == 1 })
	assert.Equal(t, map[string]int{"a": 1, "c": 3}, got)
}

func TestEntriesRoundTrip(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	es := mapx.Entries(m)
	sort.Slice(es, func(i, j int) bool { return es[i].Key < es[j].Key })
	assert.Equal(t, []mapx.Entry[string, int]{{Key: "a", Value: 1}, {Key: "b", Value: 2}}, es)
	assert.Equal(t, m, mapx.FromEntries(es))
}
```

Create `mapx/ordered_test.go`:

```go
package mapx_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/mapx"
)

func TestOrdered_InsertionOrder(t *testing.T) {
	o := mapx.NewOrdered[string, int]()
	o.Set("c", 3)
	o.Set("a", 1)
	o.Set("b", 2)
	assert.Equal(t, []string{"c", "a", "b"}, o.Keys())

	o.Set("a", 11) // update keeps position
	assert.Equal(t, []string{"c", "a", "b"}, o.Keys())
	v, ok := o.Get("a")
	assert.True(t, ok)
	assert.Equal(t, 11, v)

	o.Delete("c")
	assert.Equal(t, []string{"a", "b"}, o.Keys())
	o.Set("c", 30) // re-add appends at end
	assert.Equal(t, []string{"a", "b", "c"}, o.Keys())
	assert.Equal(t, 3, o.Len())
}

func TestOrdered_All(t *testing.T) {
	o := mapx.NewOrdered[string, int]()
	o.Set("x", 1)
	o.Set("y", 2)
	var keys []string
	var vals []int
	for k, v := range o.All() {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	assert.Equal(t, []string{"x", "y"}, keys)
	assert.Equal(t, []int{1, 2}, vals)
}

func TestOrdered_JSONPreservesOrder(t *testing.T) {
	o := mapx.NewOrdered[string, int]()
	o.Set("z", 26)
	o.Set("a", 1)
	o.Set("m", 13)
	b, err := json.Marshal(o)
	require.NoError(t, err)
	assert.Equal(t, `{"z":26,"a":1,"m":13}`, string(b), "marshal preserves insertion order")

	var got mapx.Ordered[string, int]
	require.NoError(t, json.Unmarshal([]byte(`{"z":26,"a":1,"m":13}`), &got))
	assert.Equal(t, []string{"z", "a", "m"}, got.Keys(), "unmarshal preserves source key order")
	v, ok := got.Get("m")
	assert.True(t, ok)
	assert.Equal(t, 13, v)
}

func TestOrdered_UnmarshalNull(t *testing.T) {
	var o mapx.Ordered[string, int]
	require.NoError(t, json.Unmarshal([]byte(`null`), &o))
	assert.Equal(t, 0, o.Len())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./mapx/...`
Expected: FAIL — undefined `mapx.*`.

- [ ] **Step 3: Write the implementation**

Create `mapx/mapx.go`:

```go
package mapx

// Merge returns a new map containing all keys from the given maps. When a key
// appears in more than one map the value from the later map wins.
func Merge[K comparable, V any](maps ...map[K]V) map[K]V {
	out := make(map[K]V)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// MapValues returns a new map with fn applied to each value of m.
func MapValues[K comparable, V, U any](m map[K]V, fn func(V) U) map[K]U {
	out := make(map[K]U, len(m))
	for k, v := range m {
		out[k] = fn(v)
	}
	return out
}

// Invert swaps keys and values. On duplicate values the last key wins.
func Invert[K, V comparable](m map[K]V) map[V]K {
	out := make(map[V]K, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

// Filter returns a new map of the entries of m for which pred returns true.
func Filter[K comparable, V any](m map[K]V, pred func(K, V) bool) map[K]V {
	out := make(map[K]V)
	for k, v := range m {
		if pred(k, v) {
			out[k] = v
		}
	}
	return out
}

// Entry is a single key/value pair.
type Entry[K comparable, V any] struct {
	Key   K
	Value V
}

// Entries returns the entries of m in unspecified order.
func Entries[K comparable, V any](m map[K]V) []Entry[K, V] {
	out := make([]Entry[K, V], 0, len(m))
	for k, v := range m {
		out = append(out, Entry[K, V]{Key: k, Value: v})
	}
	return out
}

// FromEntries builds a map from a slice of entries. On duplicate keys the last
// entry wins.
func FromEntries[K comparable, V any](es []Entry[K, V]) map[K]V {
	out := make(map[K]V, len(es))
	for _, e := range es {
		out[e.Key] = e.Value
	}
	return out
}
```

Create `mapx/ordered.go`:

```go
package mapx

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"iter"
	"strconv"
)

// Ordered is a map that remembers insertion order and marshals to JSON in that
// order. Updating an existing key keeps its position; deleting removes it from
// the order; re-adding appends at the end.
type Ordered[K comparable, V any] struct {
	keys []K
	m    map[K]V
}

// NewOrdered returns an empty ordered map ready for use.
func NewOrdered[K comparable, V any]() *Ordered[K, V] {
	return &Ordered[K, V]{m: make(map[K]V)}
}

func (o *Ordered[K, V]) ensure() {
	if o.m == nil {
		o.m = make(map[K]V)
	}
}

// Set inserts or updates k. Insertion appends to the key order; update keeps
// the existing position.
func (o *Ordered[K, V]) Set(k K, v V) {
	o.ensure()
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

// Get returns the value for k and whether it is present.
func (o *Ordered[K, V]) Get(k K) (V, bool) {
	v, ok := o.m[k]
	return v, ok
}

// Delete removes k, preserving the order of the remaining keys.
func (o *Ordered[K, V]) Delete(k K) {
	if _, ok := o.m[k]; !ok {
		return
	}
	delete(o.m, k)
	for i, kk := range o.keys {
		if kk == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Len returns the number of entries.
func (o *Ordered[K, V]) Len() int { return len(o.keys) }

// Keys returns a copy of the keys in insertion order.
func (o *Ordered[K, V]) Keys() []K {
	out := make([]K, len(o.keys))
	copy(out, o.keys)
	return out
}

// All iterates entries in insertion order.
func (o *Ordered[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, k := range o.keys {
			if !yield(k, o.m[k]) {
				return
			}
		}
	}
}

// MarshalJSON emits a JSON object with keys in insertion order.
func (o *Ordered[K, V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := marshalKey(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(o.m[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes a JSON object, preserving source key order. A JSON null
// is a no-op.
func (o *Ordered[K, V]) UnmarshalJSON(b []byte) error {
	o.ensure()
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil // JSON null
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("mapx: cannot unmarshal into Ordered: expected JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		ks, ok := kt.(string)
		if !ok {
			return fmt.Errorf("mapx: non-string object key")
		}
		var k K
		if err := unmarshalKey(ks, &k); err != nil {
			return err
		}
		var v V
		if err := dec.Decode(&v); err != nil {
			return err
		}
		o.Set(k, v)
	}
	if _, err := dec.Token(); err != nil { // consume closing '}'
		return err
	}
	return nil
}

// marshalKey encodes a map key as a JSON string, mirroring encoding/json's map
// key rules (string, encoding.TextMarshaler, and integer kinds as quoted
// numbers).
func marshalKey[K comparable](k K) ([]byte, error) {
	switch kv := any(k).(type) {
	case string:
		return json.Marshal(kv)
	case encoding.TextMarshaler:
		t, err := kv.MarshalText()
		if err != nil {
			return nil, err
		}
		return json.Marshal(string(t))
	default:
		b, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		if len(b) > 0 && b[0] == '"' {
			return b, nil
		}
		return json.Marshal(string(b)) // quote numeric keys, e.g. 123 -> "123"
	}
}

// unmarshalKey decodes a JSON object key string into K, mirroring marshalKey.
func unmarshalKey[K comparable](s string, k *K) error {
	switch kp := any(k).(type) {
	case *string:
		*kp = s
		return nil
	case encoding.TextUnmarshaler:
		return kp.UnmarshalText([]byte(s))
	default:
		if err := json.Unmarshal([]byte(s), k); err == nil {
			return nil
		}
		return json.Unmarshal([]byte(strconv.Quote(s)), k)
	}
}
```

Create `mapx/doc.go`:

```go
// Package mapx provides generic map helpers that the standard library maps
// package does not: Merge, MapValues, Invert, Filter, Entries/FromEntries, and
// an insertion-ordered Ordered[K,V] with order-preserving JSON.
//
// Like slicex, mapx is a gap-filler and does NOT re-implement or re-export
// stdlib maps functions (Clone, Keys, Values, Equal, Copy, DeleteFunc). Import
// "maps" directly alongside mapx.
package mapx
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./mapx/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt
go test ./mapx/... && go vet ./mapx/...
git add mapx/
git commit -m "feat(mapx): generic map gap-fillers + insertion-ordered Ordered[K,V]"
```

---

### Task 4: `set` package

**Files:**
- Create: `set/set.go`
- Create: `set/doc.go`
- Test: `set/set_test.go`

**Interfaces:**
- Consumes: stdlib `slices`, `iter`.
- Produces:
  - `type Set[T comparable]`
  - `func New[T comparable](items ...T) Set[T]`
  - `(*Set[T]).Add(items ...T)`, `(*Set[T]).Remove(items ...T)`
  - `(Set[T]).Contains(v T) bool`, `.Len() int`, `.IsEmpty() bool`
  - `(Set[T]).Union/Intersect/Diff(other Set[T]) Set[T]`, `.Equal(other Set[T]) bool`
  - `(Set[T]).Slice() []T`, `.Sorted(less func(a, b T) bool) []T`, `.All() iter.Seq[T]`

- [ ] **Step 1: Write the failing tests**

Create `set/set_test.go`:

```go
package set_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/set"
)

func TestNewAddRemoveContains(t *testing.T) {
	s := set.New(1, 2, 2, 3)
	assert.Equal(t, 3, s.Len(), "duplicates collapse")
	assert.True(t, s.Contains(2))
	s.Add(4)
	assert.True(t, s.Contains(4))
	s.Remove(2)
	assert.False(t, s.Contains(2))
	assert.False(t, s.IsEmpty())
}

func TestZeroValueUsable(t *testing.T) {
	var s set.Set[int]
	assert.True(t, s.IsEmpty())
	assert.False(t, s.Contains(1))
	s.Add(1) // must lazily allocate
	assert.True(t, s.Contains(1))
}

func TestAlgebra(t *testing.T) {
	a := set.New(1, 2, 3)
	b := set.New(2, 3, 4)
	assert.ElementsMatch(t, []int{1, 2, 3, 4}, a.Union(b).Slice())
	assert.ElementsMatch(t, []int{2, 3}, a.Intersect(b).Slice())
	assert.ElementsMatch(t, []int{1}, a.Diff(b).Slice(), "elements in a not in b")
	// operands unmodified
	assert.Equal(t, 3, a.Len())
	assert.Equal(t, 3, b.Len())
}

func TestEqual(t *testing.T) {
	assert.True(t, set.New(1, 2).Equal(set.New(2, 1)))
	assert.False(t, set.New(1, 2).Equal(set.New(1, 2, 3)))
	assert.False(t, set.New(1, 2).Equal(set.New(1, 3)))
}

func TestSortedAndAll(t *testing.T) {
	s := set.New(3, 1, 2)
	assert.Equal(t, []int{1, 2, 3}, s.Sorted(func(a, b int) bool { return a < b }))

	seen := map[int]bool{}
	for v := range s.All() {
		seen[v] = true
	}
	assert.Equal(t, map[int]bool{1: true, 2: true, 3: true}, seen)
}

func TestCopyAliasesBackingStore(t *testing.T) {
	// Documented caveat: copying a non-empty Set shares the backing map.
	a := set.New(1)
	b := a
	b.Add(2)
	assert.True(t, a.Contains(2), "documented: copy shares the backing store")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./set/...`
Expected: FAIL — undefined `set.*`.

- [ ] **Step 3: Write the implementation**

Create `set/set.go`:

```go
package set

import (
	"iter"
	"slices"
)

// Set is a generic set of comparable values backed by a map. The zero Set is
// usable (Add lazily allocates). Because it wraps a map, copying a non-empty
// Set shares the backing store — pass a *Set, or build an independent copy with
// s.Union(set.New[T]()), when you need isolation.
type Set[T comparable] struct {
	m map[T]struct{}
}

// New returns a set containing the given items.
func New[T comparable](items ...T) Set[T] {
	s := Set[T]{m: make(map[T]struct{}, len(items))}
	for _, it := range items {
		s.m[it] = struct{}{}
	}
	return s
}

// Add inserts items, lazily allocating the backing map if needed.
func (s *Set[T]) Add(items ...T) {
	if s.m == nil {
		s.m = make(map[T]struct{}, len(items))
	}
	for _, it := range items {
		s.m[it] = struct{}{}
	}
}

// Remove deletes items that are present.
func (s *Set[T]) Remove(items ...T) {
	for _, it := range items {
		delete(s.m, it)
	}
}

// Contains reports whether v is in the set.
func (s Set[T]) Contains(v T) bool {
	_, ok := s.m[v]
	return ok
}

// Len returns the number of elements.
func (s Set[T]) Len() int { return len(s.m) }

// IsEmpty reports whether the set has no elements.
func (s Set[T]) IsEmpty() bool { return len(s.m) == 0 }

// Union returns a new set with all elements of s and other.
func (s Set[T]) Union(other Set[T]) Set[T] {
	out := New[T]()
	for k := range s.m {
		out.m[k] = struct{}{}
	}
	for k := range other.m {
		out.m[k] = struct{}{}
	}
	return out
}

// Intersect returns a new set with elements present in both s and other.
func (s Set[T]) Intersect(other Set[T]) Set[T] {
	out := New[T]()
	for k := range s.m {
		if _, ok := other.m[k]; ok {
			out.m[k] = struct{}{}
		}
	}
	return out
}

// Diff returns a new set with elements in s that are not in other.
func (s Set[T]) Diff(other Set[T]) Set[T] {
	out := New[T]()
	for k := range s.m {
		if _, ok := other.m[k]; !ok {
			out.m[k] = struct{}{}
		}
	}
	return out
}

// Equal reports whether s and other contain exactly the same elements.
func (s Set[T]) Equal(other Set[T]) bool {
	if len(s.m) != len(other.m) {
		return false
	}
	for k := range s.m {
		if _, ok := other.m[k]; !ok {
			return false
		}
	}
	return true
}

// Slice returns the elements in unspecified order.
func (s Set[T]) Slice() []T {
	out := make([]T, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out
}

// Sorted returns the elements sorted by less.
func (s Set[T]) Sorted(less func(a, b T) bool) []T {
	out := s.Slice()
	slices.SortFunc(out, func(a, b T) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		default:
			return 0
		}
	})
	return out
}

// All iterates the elements in unspecified order.
func (s Set[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for k := range s.m {
			if !yield(k) {
				return
			}
		}
	}
}
```

Create `set/doc.go`:

```go
// Package set provides a generic Set[T comparable] with membership, the set
// algebra stdlib lacks (Union/Intersect/Diff/Equal), and deterministic
// iteration via Sorted. The zero Set is usable; copying a non-empty Set shares
// its backing map (documented on the type).
package set
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./set/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt
go test ./set/... && go vet ./set/...
git add set/
git commit -m "feat(set): generic Set[T] with union/intersect/diff and deterministic iteration"
```

---

### Task 5: `enum` package

**Files:**
- Create: `enum/enum.go`
- Create: `enum/doc.go`
- Test: `enum/enum_test.go`

**Interfaces:**
- Consumes: stdlib `errors`.
- Produces:
  - `var ErrInvalidValue = errors.New("enum: invalid value")`
  - `type Values[T ~string]`
  - `func New[T ~string](vals ...T) Values[T]`
  - `(Values[T]).Valid(v T) bool`, `.Parse(s string) (T, error)`, `.Values() []T`

- [ ] **Step 1: Write the failing tests**

Create `enum/enum_test.go`:

```go
package enum_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/enum"
)

type Status string

const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
)

var Statuses = enum.New(StatusActive, StatusPaused, StatusActive) // dup ignored

func TestValues_DeclaredOrderAndDedup(t *testing.T) {
	assert.Equal(t, []Status{StatusActive, StatusPaused}, Statuses.Values())
}

func TestValues_Valid(t *testing.T) {
	assert.True(t, Statuses.Valid(StatusActive))
	assert.False(t, Statuses.Valid(Status("deleted")))
}

func TestValues_Parse(t *testing.T) {
	v, err := Statuses.Parse("paused")
	require.NoError(t, err)
	assert.Equal(t, StatusPaused, v)

	_, err = Statuses.Parse("nope")
	require.Error(t, err)
	assert.True(t, errors.Is(err, enum.ErrInvalidValue))
}

func TestValues_ReturnedSliceIsCopy(t *testing.T) {
	vs := Statuses.Values()
	vs[0] = "mutated"
	assert.Equal(t, StatusActive, Statuses.Values()[0], "Values() returns an independent copy")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./enum/...`
Expected: FAIL — undefined `enum.*`.

- [ ] **Step 3: Write the implementation**

Create `enum/enum.go`:

```go
package enum

import "errors"

// ErrInvalidValue is returned by Parse for a value outside the declared set.
var ErrInvalidValue = errors.New("enum: invalid value")

// Values is an immutable, declared-once closed set of values over a named
// string type. It is distinct from set.Set (a mutable runtime collection):
// Values is a fixed value-domain.
type Values[T ~string] struct {
	ordered []T
	set     map[T]struct{}
}

// New declares a value set. Duplicate values are ignored, preserving first
// declaration order.
func New[T ~string](vals ...T) Values[T] {
	e := Values[T]{
		ordered: make([]T, 0, len(vals)),
		set:     make(map[T]struct{}, len(vals)),
	}
	for _, v := range vals {
		if _, ok := e.set[v]; ok {
			continue
		}
		e.set[v] = struct{}{}
		e.ordered = append(e.ordered, v)
	}
	return e
}

// Valid reports whether v is a declared value.
func (e Values[T]) Valid(v T) bool {
	_, ok := e.set[v]
	return ok
}

// Parse converts s to T when it is a declared value, else returns
// ErrInvalidValue.
func (e Values[T]) Parse(s string) (T, error) {
	v := T(s)
	if _, ok := e.set[v]; ok {
		return v, nil
	}
	var zero T
	return zero, ErrInvalidValue
}

// Values returns a copy of the declared values in declaration order.
func (e Values[T]) Values() []T {
	out := make([]T, len(e.ordered))
	copy(out, e.ordered)
	return out
}
```

Create `enum/doc.go`:

```go
// Package enum provides Values[T ~string], a fixed closed set of values over a
// named string type, declared once via New. It offers Valid, Parse, and Values
// without per-enum boilerplate. Unlike set.Set (a mutable collection), enum is
// an immutable declared value-domain.
package enum
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./enum/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt
go test ./enum/... && go vet ./enum/...
git add enum/
git commit -m "feat(enum): declared closed value-set Values[T ~string]"
```

---

### Task 6: `stringsx` package

**Files:**
- Create: `stringsx/case.go` (ToSnake/ToCamel/ToKebab + helpers)
- Create: `stringsx/stringsx.go` (Truncate/Ellipsis/TruncateWords/Mask/Pluralize)
- Create: `stringsx/doc.go`
- Test: `stringsx/case_test.go`
- Test: `stringsx/stringsx_test.go`

**Interfaces:**
- Consumes: stdlib `strings`, `unicode`, `unicode/utf8`.
- Produces:
  - `func ToSnake(s string) string`, `func ToKebab(s string) string`, `func ToCamel(s string) string`, `func ToCamelWith(s string, acronyms ...string) string`
  - `func Truncate(s string, n int) string`, `func Ellipsis(s string, n int) string`, `func TruncateWords(s string, n int) string`
  - `func Mask(s string, keep int) string`, `func Pluralize(word string, n int) string`

- [ ] **Step 1: Write the failing tests**

Create `stringsx/case_test.go`:

```go
package stringsx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/stringsx"
)

func TestToSnake(t *testing.T) {
	cases := map[string]string{
		"UserID":      "user_id",
		"HTTPServer":  "http_server",
		"userName":    "user_name",
		"Already_Snake": "already_snake",
		"kebab-case":  "kebab_case",
		"with space":  "with_space",
		"":            "",
	}
	for in, want := range cases {
		assert.Equal(t, want, stringsx.ToSnake(in), "ToSnake(%q)", in)
	}
}

func TestToKebab(t *testing.T) {
	assert.Equal(t, "user-id", stringsx.ToKebab("UserID"))
	assert.Equal(t, "http-server", stringsx.ToKebab("HTTPServer"))
	assert.Equal(t, "user-name", stringsx.ToKebab("user_name"))
}

func TestToCamel(t *testing.T) {
	cases := map[string]string{
		"user_id":    "userId", // mechanical: no acronym special-casing
		"user-name":  "userName",
		"HTTP server": "httpServer",
		"Already":    "already",
		"":           "",
	}
	for in, want := range cases {
		assert.Equal(t, want, stringsx.ToCamel(in), "ToCamel(%q)", in)
	}
}

func TestToCamelWith(t *testing.T) {
	assert.Equal(t, "userID", stringsx.ToCamelWith("user_id", "ID"))
	assert.Equal(t, "apiURL", stringsx.ToCamelWith("api_url", "URL"))
	assert.Equal(t, "getUserOAuthToken", stringsx.ToCamelWith("get_user_oauth_token", "OAuth"))
	assert.Equal(t, "idToken", stringsx.ToCamelWith("id_token", "ID"), "leading word always lowercased")
	assert.Equal(t, "userName", stringsx.ToCamelWith("user_name", "ID"), "unmatched words title-cased")
}
```

Create `stringsx/stringsx_test.go`:

```go
package stringsx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/stringsx"
)

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", stringsx.Truncate("abcdef", 3))
	assert.Equal(t, "abc", stringsx.Truncate("abc", 5), "shorter than n unchanged")
	assert.Equal(t, "", stringsx.Truncate("abc", 0))
	assert.Equal(t, "hél", stringsx.Truncate("héllo", 3), "rune-safe (é is one rune)")
}

func TestEllipsis(t *testing.T) {
	assert.Equal(t, "abc…", stringsx.Ellipsis("abcdef", 3))
	assert.Equal(t, "abc", stringsx.Ellipsis("abc", 3), "exact length not truncated")
	assert.Equal(t, "", stringsx.Ellipsis("abc", 0))
}

func TestTruncateWords(t *testing.T) {
	assert.Equal(t, "one two", stringsx.TruncateWords("one two three four", 2))
	assert.Equal(t, "one two", stringsx.TruncateWords("one two", 5), "fewer words unchanged")
	assert.Equal(t, "", stringsx.TruncateWords("one two", 0))
}

func TestMask(t *testing.T) {
	assert.Equal(t, "******123", stringsx.Mask("secret123", 3))
	assert.Equal(t, "*********", stringsx.Mask("secret123", 0), "keep<=0 masks all")
	assert.Equal(t, "*****", stringsx.Mask("short", 10), "keep>=len masks all (no leak)")
	assert.Equal(t, "****", stringsx.Mask("café", 0), "rune count, not byte count")
}

func TestPluralize(t *testing.T) {
	cases := []struct {
		word string
		n    int
		want string
	}{
		{"cat", 2, "cats"},
		{"box", 2, "boxes"},
		{"bus", 2, "buses"},
		{"church", 2, "churches"},
		{"city", 2, "cities"},
		{"day", 2, "days"}, // vowel+y keeps y
		{"cat", 1, "cat"},  // n==1 unchanged
		{"", 2, ""},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, stringsx.Pluralize(c.word, c.n), "Pluralize(%q,%d)", c.word, c.n)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./stringsx/...`
Expected: FAIL — undefined `stringsx.*`.

- [ ] **Step 3: Write the implementations**

Create `stringsx/case.go`:

```go
package stringsx

import (
	"strings"
	"unicode"
)

// splitWords breaks s into words on separators (space, '_', '-') and camelCase
// / acronym boundaries.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	runes := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for i, r := range runes {
		switch {
		case r == ' ' || r == '_' || r == '-':
			flush()
		case unicode.IsUpper(r):
			prev := rune(0)
			if i > 0 {
				prev = runes[i-1]
			}
			next := rune(0)
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			// Boundary before an uppercase that follows a lower/digit, or that
			// starts a new word after an acronym (UPPERlower).
			if len(cur) > 0 && (unicode.IsLower(prev) || unicode.IsDigit(prev) ||
				(unicode.IsUpper(prev) && unicode.IsLower(next))) {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

func toDelimited(s string, delim string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, delim)
}

// ToSnake converts s to snake_case.
func ToSnake(s string) string { return toDelimited(s, "_") }

// ToKebab converts s to kebab-case.
func ToKebab(s string) string { return toDelimited(s, "-") }

// ToCamel converts s to lowerCamelCase mechanically: each word after the first
// is title-cased ("user_id" -> "userId"). It does not special-case acronyms —
// use ToCamelWith to supply them.
func ToCamel(s string) string { return ToCamelWith(s) }

// ToCamelWith is ToCamel with caller-supplied acronyms. Each acronym is matched
// case-insensitively against a word after the first and rendered with the
// acronym's own spelling, so ToCamelWith("user_id", "ID") == "userID". The first
// word is always lowercased (lowerCamelCase). Unmatched words are title-cased.
func ToCamelWith(s string, acronyms ...string) string {
	var rules map[string]string
	if len(acronyms) > 0 {
		rules = make(map[string]string, len(acronyms))
		for _, a := range acronyms {
			rules[strings.ToLower(a)] = a
		}
	}
	words := splitWords(s)
	var b strings.Builder
	for i, w := range words {
		if i == 0 {
			b.WriteString(strings.ToLower(w))
			continue
		}
		lower := strings.ToLower(w)
		if rep, ok := rules[lower]; ok {
			b.WriteString(rep)
			continue
		}
		r := []rune(lower)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}
```

Create `stringsx/stringsx.go`:

```go
package stringsx

import "strings"

// Truncate returns the first n runes of s. n <= 0 returns "".
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Ellipsis returns the first n runes of s, appending "…" when s was longer.
// n <= 0 returns "".
func Ellipsis(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// TruncateWords returns the first n whitespace-separated words of s. Inputs
// with n or fewer words are returned unchanged. n <= 0 returns "".
func TruncateWords(s string, n int) string {
	if n <= 0 {
		return ""
	}
	fields := strings.Fields(s)
	if len(fields) <= n {
		return s
	}
	return strings.Join(fields[:n], " ")
}

// Mask keeps the last keep runes of s and replaces the rest with '*'. When keep
// >= len(s) or keep <= 0 the whole string is masked (never leaks characters).
func Mask(s string, keep int) string {
	r := []rune(s)
	if keep <= 0 || keep >= len(r) {
		return strings.Repeat("*", len(r))
	}
	return strings.Repeat("*", len(r)-keep) + string(r[len(r)-keep:])
}

// Pluralize returns the naive English plural of word for count n. It is a
// best-effort helper for trusted, developer-facing strings only: append "s";
// "es" after s/x/z/ch/sh; consonant + "y" -> "ies". It is NOT a linguistics
// engine (no irregular plurals). Locale-aware pluralization belongs to the
// future i18n package. Returns word unchanged when n == 1.
func Pluralize(word string, n int) string {
	if n == 1 || word == "" {
		return word
	}
	lower := strings.ToLower(word)
	switch {
	case strings.HasSuffix(lower, "s"), strings.HasSuffix(lower, "x"),
		strings.HasSuffix(lower, "z"), strings.HasSuffix(lower, "ch"),
		strings.HasSuffix(lower, "sh"):
		return word + "es"
	case strings.HasSuffix(lower, "y") && len(lower) >= 2 && !isVowel(rune(lower[len(lower)-2])):
		return word[:len(word)-1] + "ies"
	default:
		return word + "s"
	}
}

func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}
```

Create `stringsx/doc.go`:

```go
// Package stringsx provides string-shaping helpers stdlib lacks: case
// conversion (ToSnake/ToCamel/ToKebab), rune-safe Truncate/Ellipsis/
// TruncateWords, PII Mask, and a naive English Pluralize.
//
// stringsx is for TRUSTED, developer-facing strings. Untrusted input belongs to
// the sanitize package; locale-aware pluralization belongs to the future i18n
// package (multi-language + custom plural rules) — do not reach for Pluralize
// when you need real localization.
package stringsx
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./stringsx/...`
Expected: PASS. If a case-conversion edge case fails, adjust `splitWords` boundary logic (the acronym `UPPERlower` split is the subtle case) until the `TestToSnake`/`TestToCamel` tables are green.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt
go test ./stringsx/... && go vet ./stringsx/...
git add stringsx/
git commit -m "feat(stringsx): case conversion, rune-safe truncation, Mask, naive Pluralize"
```

---

### Task 7: `encoding` package

**Files:**
- Create: `encoding/base62.go`
- Create: `encoding/crockford.go`
- Create: `encoding/errors.go`
- Create: `encoding/doc.go`
- Test: `encoding/base62_test.go`
- Test: `encoding/crockford_test.go`

**Interfaces:**
- Consumes: stdlib `math/big`, `strings`, `errors`.
- Produces:
  - `var ErrInvalidEncoding = errors.New("encoding: invalid input")`
  - `func EncodeInt(n uint64) string`, `func DecodeInt(s string) (uint64, error)`
  - `func Encode(b []byte) string`, `func Decode(s string) ([]byte, error)`
  - `func Encode32(b []byte) string`, `func Decode32(s string) ([]byte, error)`
- Note: Base62 uses Go's `big.Int.Text(62)` digit order — `0-9a-zA-Z` — so `EncodeInt`, `Encode`, and the `big.Int` byte path all share one alphabet. Crockford uses `0123456789ABCDEFGHJKMNPQRSTVWXYZ` (excludes I/L/O/U), MSB-first with leading (MSB) zero-bit padding — this is the ULID-canonical layout so that a 16-byte input yields 26 chars and a 10-byte input yields 16 chars, matching `id`.

- [ ] **Step 1: Write the failing tests**

Create `encoding/base62_test.go`:

```go
package encoding_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/encoding"
)

func TestEncodeDecodeInt(t *testing.T) {
	for _, n := range []uint64{0, 1, 61, 62, 1234567890, ^uint64(0)} {
		s := encoding.EncodeInt(n)
		got, err := encoding.DecodeInt(s)
		require.NoError(t, err)
		assert.Equal(t, n, got, "round-trip %d via %q", n, s)
	}
	assert.Equal(t, "0", encoding.EncodeInt(0))
}

func TestDecodeInt_Invalid(t *testing.T) {
	_, err := encoding.DecodeInt("")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
	_, err = encoding.DecodeInt("!!")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
}

func TestEncodeDecodeBytes_RoundTripWithLeadingZeros(t *testing.T) {
	cases := [][]byte{
		{},
		{0x00},
		{0x00, 0x00, 0x01},
		{0xff, 0xff, 0xff},
		[]byte("hello world"),
	}
	for _, b := range cases {
		s := encoding.Encode(b)
		got, err := encoding.Decode(s)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(b, got), "round-trip %x via %q -> %x", b, s, got)
	}
}
```

Create `encoding/crockford_test.go`:

```go
package encoding_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/encoding"
)

func TestEncode32_Widths(t *testing.T) {
	assert.Len(t, encoding.Encode32(make([]byte, 16)), 26, "16 bytes -> 26 chars (ULID width)")
	assert.Len(t, encoding.Encode32(make([]byte, 10)), 16, "10 bytes -> 16 chars (Short width)")
	assert.Equal(t, "", encoding.Encode32(nil))
}

func TestEncode32_RoundTrip(t *testing.T) {
	cases := [][]byte{
		{0x00},
		{0xff},
		bytes.Repeat([]byte{0xAB}, 16),
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	}
	for _, b := range cases {
		s := encoding.Encode32(b)
		got, err := encoding.Decode32(s)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(b, got), "round-trip %x via %q -> %x", b, s, got)
	}
}

func TestDecode32_AliasesAndCase(t *testing.T) {
	// I/L -> 1, O -> 0, case-insensitive; the two spellings decode identically.
	a, err := encoding.Decode32("ABCDEFGH")
	require.NoError(t, err)
	b, err := encoding.Decode32("abcdefgh")
	require.NoError(t, err)
	assert.True(t, bytes.Equal(a, b), "decode is case-insensitive")

	withAlias, err := encoding.Decode32("0O1I1L") // O->0, I->1, L->1
	require.NoError(t, err)
	canonical, err := encoding.Decode32("001111")
	require.NoError(t, err)
	assert.True(t, bytes.Equal(withAlias, canonical), "I/L->1, O->0 aliases")
}

func TestDecode32_InvalidChar(t *testing.T) {
	_, err := encoding.Decode32("U") // U is excluded from Crockford
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
	_, err = encoding.Decode32("!")
	assert.ErrorIs(t, err, encoding.ErrInvalidEncoding)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./encoding/...`
Expected: FAIL — undefined `encoding.*`.

- [ ] **Step 3: Write the implementations**

Create `encoding/errors.go`:

```go
package encoding

import "errors"

// ErrInvalidEncoding is returned when input contains characters outside the
// codec's alphabet or otherwise cannot be decoded.
var ErrInvalidEncoding = errors.New("encoding: invalid input")
```

Create `encoding/base62.go`:

```go
package encoding

import (
	"math/big"
	"strings"
)

// base62Alphabet matches math/big's Text(62) digit order (0-9, a-z, A-Z) so
// EncodeInt, Encode, and the big.Int byte path share one alphabet.
const base62Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// EncodeInt encodes n in base62.
func EncodeInt(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [11]byte // ceil(64/log2(62)) = 11
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Alphabet[n%62]
		n /= 62
	}
	return string(buf[i:])
}

// DecodeInt decodes a base62 string into a uint64, returning ErrInvalidEncoding
// on an invalid character or overflow.
func DecodeInt(s string) (uint64, error) {
	if s == "" {
		return 0, ErrInvalidEncoding
	}
	const maxUint64 = ^uint64(0)
	var n uint64
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(base62Alphabet, s[i])
		if idx < 0 {
			return 0, ErrInvalidEncoding
		}
		if n > (maxUint64-uint64(idx))/62 {
			return 0, ErrInvalidEncoding
		}
		n = n*62 + uint64(idx)
	}
	return n, nil
}

// Encode encodes an arbitrary byte slice in base62. Leading zero bytes are
// preserved by emitting one leading '0' per zero byte (base58-style), so
// Decode(Encode(b)) == b for any b.
func Encode(b []byte) string {
	z := 0
	for z < len(b) && b[z] == 0 {
		z++
	}
	var digits string
	if z < len(b) {
		digits = new(big.Int).SetBytes(b[z:]).Text(62)
	}
	return strings.Repeat("0", z) + digits
}

// Decode reverses Encode.
func Decode(s string) ([]byte, error) {
	z := 0
	for z < len(s) && s[z] == '0' {
		z++
	}
	out := make([]byte, z) // z leading zero bytes
	rest := s[z:]
	if rest == "" {
		return out, nil
	}
	n, ok := new(big.Int).SetString(rest, 62)
	if !ok {
		return nil, ErrInvalidEncoding
	}
	return append(out, n.Bytes()...), nil
}
```

Create `encoding/crockford.go`:

```go
package encoding

// crockfordAlphabet is Crockford base32: digits 0-9 then A-Z excluding I, L, O,
// U. Index == 5-bit value.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Encode32 encodes b in Crockford base32, MSB-first with leading (most
// significant) zero-bit padding. This is the ULID-canonical layout: 16 bytes ->
// 26 chars, 10 bytes -> 16 chars.
func Encode32(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	nbits := len(b) * 8
	nchars := (nbits + 4) / 5 // ceil(nbits/5)
	pad := nchars*5 - nbits   // padding bits sit at the MSB (front)
	bit := func(pos int) int {
		dpos := pos - pad
		if dpos < 0 {
			return 0
		}
		return int((b[dpos/8] >> uint(7-dpos%8)) & 1)
	}
	out := make([]byte, nchars)
	for i := range out {
		v := 0
		for k := 0; k < 5; k++ {
			v = v<<1 | bit(i*5+k)
		}
		out[i] = crockfordAlphabet[v]
	}
	return string(out)
}

// Decode32 reverses Encode32. It is case-insensitive and applies Crockford
// decode aliases (I,i,L,l -> 1; O,o -> 0). Invalid characters (including U)
// return ErrInvalidEncoding.
func Decode32(s string) ([]byte, error) {
	if s == "" {
		return []byte{}, nil
	}
	nbits := len(s) * 5
	nbytes := nbits / 8
	pad := nbits - nbytes*8 // leading pad bits to drop
	out := make([]byte, nbytes)
	pos := 0
	for i := 0; i < len(s); i++ {
		v, ok := decodeCrockfordChar(s[i])
		if !ok {
			return nil, ErrInvalidEncoding
		}
		for k := 4; k >= 0; k-- {
			p := pos
			pos++
			if p < pad {
				continue // drop MSB padding bit
			}
			if (v>>uint(k))&1 == 1 {
				dpos := p - pad
				out[dpos/8] |= 1 << uint(7-dpos%8)
			}
		}
	}
	return out, nil
}

func decodeCrockfordChar(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c == 'O' || c == 'o':
		return 0, true
	case c == 'I' || c == 'i' || c == 'L' || c == 'l':
		return 1, true
	}
	uc := c
	if uc >= 'a' && uc <= 'z' {
		uc -= 'a' - 'A'
	}
	for i := 0; i < len(crockfordAlphabet); i++ {
		if crockfordAlphabet[i] == uc {
			return i, true
		}
	}
	return 0, false
}
```

Create `encoding/doc.go`:

```go
// Package encoding provides compact, URL-safe, human-typable codecs: base62 for
// integers and byte slices, and Crockford base32 (excludes I/L/O/U) for
// sortable IDs and short codes. The Crockford codec uses the ULID-canonical
// MSB-first, left-padded layout so a 16-byte value encodes to 26 characters and
// a 10-byte value to 16.
package encoding
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./encoding/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt
go test ./encoding/... && go vet ./encoding/...
git add encoding/
git commit -m "feat(encoding): base62 + Crockford base32 codecs (ULID-canonical layout)"
```

---

### Task 8: gate the `id → encoding` refactor

This task attempts to replace `id`'s private Crockford codec with the shared `encoding` package, keeping the change **only if** it passes two hard checks. Either outcome is a success.

**Files:**
- Create: `encoding/id_compat_test.go` (cross-check, lives in `encoding` to avoid an `id`→`encoding` import until the switch is committed)
- Read: `id/ulid.go`, `id/short.go`, `id/codec.go`, `id/alloc_test.go`, `id/*_test.go`
- Modify (only if both gates pass): `id/codec.go` (and callers) to delegate to `encoding`

**Interfaces:**
- Consumes: `encoding.Encode32`/`Decode32`, `id.ULID`/`id.Short` + their `String()`/`Parse*`.
- Produces: no new exported API. Adds the `id → encoding` internal edge iff kept.

- [ ] **Step 1: Write the compatibility cross-check (Gate 1: byte-for-byte)**

First inspect `id` to confirm the exact `String()`/`Parse` entry points and the known-answer vectors already asserted in `id`'s tests:

Run: `sed -n '1,80p' id/ulid.go id/short.go id/codec.go`

Create `encoding/id_compat_test.go` — assert `encoding.Encode32`/`Decode32` reproduce `id`'s ULID and Short strings for the existing KAT plus random samples. Use `id`'s current public API to generate values:

```go
package encoding_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/encoding"
	"github.com/dmitrymomot/forge/id"
)

// Gate 1: the shared codec must reproduce id's existing output exactly.
func TestEncoding_MatchesIDULID(t *testing.T) {
	for i := 0; i < 1000; i++ {
		u := id.NewULID()
		// id.ULID is a [16]byte value type; its String() is the canonical form.
		assert.Equal(t, u.String(), encoding.Encode32(u[:]),
			"encoding.Encode32 must match id.ULID.String()")

		back, err := encoding.Decode32(u.String())
		require.NoError(t, err)
		assert.Equal(t, u[:], back, "encoding.Decode32 must round-trip id's string to id's bytes")
	}
}

func TestEncoding_MatchesIDShort(t *testing.T) {
	for i := 0; i < 1000; i++ {
		s := id.NewShort()
		assert.Equal(t, s.String(), encoding.Encode32(s[:]),
			"encoding.Encode32 must match id.Short.String()")
	}
}
```

> If `id.ULID`/`id.Short` are not directly indexable as `u[:]` (e.g. an unexported backing array), adapt via their `MarshalText`/byte accessor as defined in `id`. The assertion is: `encoding.Encode32(<the 16 or 10 raw bytes>) == <id's String()>`.

- [ ] **Step 2: Run Gate 1**

Run: `go test ./encoding/ -run TestEncoding_MatchesID -v`

- **PASS** → the shared codec is byte-compatible; proceed to Step 3.
- **FAIL** → the layouts differ. **Stop the refactor.** Delete `encoding/id_compat_test.go`, leave `id` untouched, and record in the commit/PR notes that `encoding` ships standalone (no `id` edge). Skip to Step 6.

- [ ] **Step 3: Attempt the refactor + measure allocations (Gate 2)**

If Gate 1 passed, change `id`'s codec to delegate to `encoding` (replace the private Crockford encode/decode in `id/codec.go` with calls to `encoding.Encode32`/`encoding.Decode32`, keeping `id`'s public `String()`/`Parse*` signatures identical). Then run `id`'s full existing suite, including its allocation assertions and benchmarks.

Run:
```bash
go test ./id/... -run . -count=1
go test ./id/... -run TestAlloc -v            # id's existing alloc assertions
go test ./id/... -bench=String -benchmem -run=^$
```

Compare `String()` allocations against `id`'s documented bound (`<= 1`).

- [ ] **Step 4: Decide — keep or revert (the gate)**

- **Gate 2 PASS** (`id`'s full suite green AND `String()` allocations still `<= 1`, no material slowdown): **keep** the refactor. The `id → encoding` edge is now real.
- **Gate 2 FAIL** (any `id` test red, or `String()` now allocates `> 1` because `encoding.Encode32` returns a freshly allocated string): **revert** `id/codec.go` to its private codec (`git checkout -- id/`). `encoding` remains standalone. This is an expected, acceptable outcome — do not force it. (Optional future path, not done here: give `encoding` an `AppendEncode32(dst, b []byte) []byte` so `id` can encode into a stack buffer with one allocation; deferred as YAGNI unless the shared edge is specifically wanted.)

- [ ] **Step 5: If kept — verify the whole suite is green**

Run: `just check`
Expected: all packages pass, lint clean.

- [ ] **Step 6: Commit the outcome**

If **kept**:
```bash
just fmt
git add id/ encoding/
git commit -m "refactor(id): delegate Crockford codec to encoding (byte-compatible, allocs within bound)"
```

If **reverted** (Gate 1 or Gate 2 failed):
```bash
# id/ already restored via git checkout; only remove the temporary cross-check.
git rm -f encoding/id_compat_test.go 2>/dev/null || rm -f encoding/id_compat_test.go
git add -A
git commit -m "docs(encoding): id keeps its tuned private codec; encoding ships standalone (gate not met)"
```

---

## Final review (whole batch)

- [ ] Run the full suite once more: `just check` — every new package green, lint/nilaway/betteralign/modernize clean.
- [ ] Confirm `go.mod` gained **no** new external dependencies (only stdlib was used).
- [ ] Confirm each `doc.go` states the no-stdlib-aliasing rationale (`slicex`, `mapx`, `set`) and the `sanitize`/`i18n` boundaries (`stringsx`).
- [ ] Dispatch a final review subagent (opus) over the whole diff against this plan and the spec.
