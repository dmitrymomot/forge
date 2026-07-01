# structfields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Provide the one sanctioned reflection primitive `Walk(v, tagKey, fn)` that visits each exported field of a struct (or non-nil `*struct`) once, exposing name, parsed struct tag, `reflect.Value`, and a settable `Set` closure — confining all `reflect` usage to this single audited package.

**Architecture:** A tiny stateless package with three concerns split across files: `tag.go` (the `Tag` value type + parser + `Ignored`/`HasOption`), `structfields.go` (the `Field` type and the `Walk` traversal with the `Set` closure), and `errors.go` (the two sentinels). All functions are free functions / value types; there are no instances or builders. `reflect.Value`/`reflect.StructTag` are stdlib and are the framework's single permitted public reflection surface.

**Tech Stack:** stdlib only — `reflect`, `strings`, `errors`, `fmt`. Tests use `github.com/stretchr/testify` (assert/require), matching neighbor packages.

## Global Constraints

- Module: github.com/dmitrymomot/forge. Go 1.26.
- Go 1.26: use the new(expr) builtin (never a ptr.To wrapper). Run modernize before done.
- Minimal deps, not zero: stdlib-only UNLESS the spec's deps table lists an external dep for this package. Isolate any real dep. No new external deps beyond what the spec names.
- Flat, package-per-directory. Files split by concern. doc.go = package doc with scope + an explicit "what this is NOT" + sibling pointers.
- errors.go: errors.Is-matchable single-line sentinels prefixed "pkg: ", wrapped with %w. No stack traces / no multi-line error blobs.
- Black-box tests ONLY (package X_test). Table-driven. Known-answer vectors where a public standard/oracle exists. testify (github.com/stretchr/testify assert/require) is available and used by several packages; match the neighbors you study.
- No builders. Free funcs / generic value types / functional options only. Public methods never return unexported types.
- Conventional commit messages. NEVER add any Claude attribution, "Generated with", or "Co-Authored-By" line.
- Definition of done: "just check" (fmt + lint + test with -race) is clean, including go vet, golangci-lint, nilaway, betteralign, modernize.

---

### Task 1: `Tag` value type + parser

**Files:**
- Create: `structfields/tag.go`
- Test: `structfields/tag_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `type Tag struct { Name string; Options []string; Raw string }`
  - `func (t Tag) Ignored() bool`
  - `func (t Tag) HasOption(opt string) bool`
  - `func parseTag(raw string) Tag` (unexported; consumed by `Walk` in Task 3)

- [ ] **Step 1: Write the failing test**

```go
package structfields_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/structfields"
)

func TestTag_Ignored(t *testing.T) {
	assert.True(t, structfields.Tag{Name: "-"}.Ignored())
	assert.False(t, structfields.Tag{Name: "field"}.Ignored())
	assert.False(t, structfields.Tag{Name: ""}.Ignored())
}

func TestTag_HasOption(t *testing.T) {
	tg := structfields.Tag{Name: "field", Options: []string{"omitempty", "string"}}
	assert.True(t, tg.HasOption("omitempty"))
	assert.True(t, tg.HasOption("string"))
	assert.False(t, tg.HasOption("required"))
	assert.False(t, tg.HasOption(""))

	empty := structfields.Tag{Name: "field"}
	assert.False(t, empty.HasOption("omitempty"), "nil Options never matches")
}
```

- [ ] **Step 2: Run it, verify it fails**

Command: `go test ./structfields/ -run 'TestTag' -v`

Expected FAIL: compilation error — `undefined: structfields.Tag` (the package/type does not exist yet).

- [ ] **Step 3: Write the minimal implementation**

`structfields/tag.go`:

```go
package structfields

import "strings"

// Tag is a parsed struct tag for a single tagKey. Name is the first
// comma-separated segment (empty when the tag is absent); Options holds the
// remaining segments; Raw is the unparsed tag value for tagKey.
type Tag struct {
	Name    string   // first comma-segment of the tag ("" when absent)
	Options []string // remaining comma-separated segments
	Raw     string   // raw tag value for tagKey
}

// Ignored reports whether the tag explicitly excludes the field (Name == "-").
func (t Tag) Ignored() bool {
	return t.Name == "-"
}

// HasOption reports whether opt appears among the tag's Options.
func (t Tag) HasOption(opt string) bool {
	for _, o := range t.Options {
		if o == opt {
			return true
		}
	}
	return false
}

// parseTag splits a raw struct-tag value into Name + Options. An empty raw
// value yields a zero Tag (Name == "", nil Options).
func parseTag(raw string) Tag {
	if raw == "" {
		return Tag{Raw: raw}
	}
	parts := strings.Split(raw, ",")
	return Tag{
		Name:    parts[0],
		Options: parts[1:],
		Raw:     raw,
	}
}
```

- [ ] **Step 4: Run tests, verify pass**

Command: `go test ./structfields/ -run 'TestTag' -v`

Expected: `ok  	github.com/dmitrymomot/forge/structfields`

- [ ] **Step 5: Commit**

```
git add structfields/tag.go structfields/tag_test.go
git commit -m "feat(structfields): add Tag value type with parser, Ignored, HasOption"
```

---

### Task 2: error sentinels

**Files:**
- Create: `structfields/errors.go`
- Test: `structfields/errors_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `var ErrNotStruct = errors.New("structfields: not a struct")`
  - `var ErrNotSettable = errors.New("structfields: field not settable")`

- [ ] **Step 1: Write the failing test**

```go
package structfields_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/structfields"
)

func TestSentinels_Messages(t *testing.T) {
	assert.EqualError(t, structfields.ErrNotStruct, "structfields: not a struct")
	assert.EqualError(t, structfields.ErrNotSettable, "structfields: field not settable")
}
```

- [ ] **Step 2: Run it, verify it fails**

Command: `go test ./structfields/ -run 'TestSentinels' -v`

Expected FAIL: compilation error — `undefined: structfields.ErrNotStruct` / `undefined: structfields.ErrNotSettable`.

- [ ] **Step 3: Write the minimal implementation**

`structfields/errors.go`:

```go
package structfields

import "errors"

// ErrNotStruct is returned by Walk when v is neither a struct nor a non-nil
// pointer to a struct.
var ErrNotStruct = errors.New("structfields: not a struct")

// ErrNotSettable is returned by a Field's Set when the target value cannot be
// assigned — either because Walk received a non-pointer struct (read-only) or
// the field is otherwise unsettable.
var ErrNotSettable = errors.New("structfields: field not settable")
```

- [ ] **Step 4: Run tests, verify pass**

Command: `go test ./structfields/ -run 'TestSentinels' -v`

Expected: `ok  	github.com/dmitrymomot/forge/structfields`

- [ ] **Step 5: Commit**

```
git add structfields/errors.go structfields/errors_test.go
git commit -m "feat(structfields): add ErrNotStruct and ErrNotSettable sentinels"
```

---

### Task 3: `Field` type + `Walk` traversal (read-only value struct)

**Files:**
- Create: `structfields/structfields.go`
- Test: `structfields/structfields_test.go`

**Interfaces:**
- Consumes: `parseTag(raw string) Tag` (Task 1); `ErrNotStruct`, `ErrNotSettable` (Task 2).
- Produces:
  - `type Field struct { Name string; Tag Tag; Value reflect.Value; Set func(v any) error }`
  - `func Walk(v any, tagKey string, fn func(Field) error) error`

This task delivers `Walk` for a struct **or** non-nil `*struct`, visiting only exported fields shallowly, wiring `Tag`, `Value`, and a `Set` closure. For a value (non-pointer) struct the field is read-only and `Set` returns `ErrNotSettable`. Settable-pointer behaviour and error propagation are asserted here too, so this is the primary deliverable; Task 4 adds edge-case hardening only.

- [ ] **Step 1: Write the failing test**

```go
package structfields_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/structfields"
)

type walkSample struct {
	Name    string `env:"NAME,required"`
	Age     int    `env:"AGE"`
	Ignored string `env:"-"`
	NoTag   bool
	private string // unexported: never visited
}

func TestWalk_VisitsExportedFieldsWithParsedTags(t *testing.T) {
	var s walkSample
	var got []structfields.Field
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		got = append(got, f)
		return nil
	})
	require.NoError(t, err)

	require.Len(t, got, 4, "4 exported fields, unexported private skipped")

	assert.Equal(t, "Name", got[0].Name)
	assert.Equal(t, "NAME", got[0].Tag.Name)
	assert.Equal(t, []string{"required"}, got[0].Tag.Options)
	assert.True(t, got[0].Tag.HasOption("required"))
	assert.Equal(t, "NAME,required", got[0].Tag.Raw)

	assert.Equal(t, "Age", got[1].Name)
	assert.Equal(t, "AGE", got[1].Tag.Name)

	assert.Equal(t, "Ignored", got[2].Name)
	assert.True(t, got[2].Tag.Ignored())

	assert.Equal(t, "NoTag", got[3].Name)
	assert.Equal(t, "", got[3].Tag.Name, "absent tag yields empty Name")
	assert.Nil(t, got[3].Tag.Options)
}

func TestWalk_PointerFieldsAreSettable(t *testing.T) {
	var s walkSample
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		switch f.Name {
		case "Name":
			return f.Set("hello")
		case "Age":
			return f.Set(42)
		default:
			return nil
		}
	})
	require.NoError(t, err)
	assert.Equal(t, "hello", s.Name)
	assert.Equal(t, 42, s.Age)
}

func TestWalk_SetConvertsAssignableTypes(t *testing.T) {
	type nums struct {
		N int64
	}
	var s nums
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		return f.Set(int(7)) // int convertible to int64
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), s.N)
}

func TestWalk_SetTypeMismatchReturnsError(t *testing.T) {
	var s walkSample
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		if f.Name == "Age" {
			return f.Set("not-a-number")
		}
		return nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "structfields:")
	assert.Contains(t, err.Error(), "Age")
}

func TestWalk_ValueStructIsReadOnly(t *testing.T) {
	s := walkSample{Name: "orig"}
	err := structfields.Walk(s, "env", func(f structfields.Field) error {
		if f.Name == "Name" {
			assert.False(t, f.Value.CanSet(), "value-struct field is not settable")
			setErr := f.Set("mutated")
			assert.True(t, errors.Is(setErr, structfields.ErrNotSettable))
			return setErr
		}
		return nil
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, structfields.ErrNotSettable))
	assert.Equal(t, "orig", s.Name, "value struct not mutated")
}

func TestWalk_ValueReflectsField(t *testing.T) {
	s := walkSample{Age: 9}
	err := structfields.Walk(s, "env", func(f structfields.Field) error {
		if f.Name == "Age" {
			assert.Equal(t, reflect.Int, f.Value.Kind())
			assert.Equal(t, int64(9), f.Value.Int())
		}
		return nil
	})
	require.NoError(t, err)
}

func TestWalk_PropagatesCallbackError(t *testing.T) {
	sentinel := errors.New("stop")
	var visited int
	err := structfields.Walk(&walkSample{}, "env", func(f structfields.Field) error {
		visited++
		return sentinel
	})
	assert.Equal(t, 1, visited, "traversal stops on first callback error")
	assert.True(t, errors.Is(err, sentinel))
}
```

- [ ] **Step 2: Run it, verify it fails**

Command: `go test ./structfields/ -run 'TestWalk' -v`

Expected FAIL: compilation error — `undefined: structfields.Walk` and `undefined: structfields.Field`.

- [ ] **Step 3: Write the minimal implementation**

`structfields/structfields.go`:

```go
package structfields

import (
	"fmt"
	"reflect"
)

// Field is one exported struct field surfaced by Walk.
type Field struct {
	Name  string        // Go field name
	Tag   Tag           // parsed tagKey tag
	Value reflect.Value // settable when Walk received a non-nil *struct
	Set   func(v any) error
}

// Walk visits each exported field of a struct (or non-nil *struct) exactly
// once, in declaration order, invoking fn with a Field carrying the field's
// name, its parsed tagKey tag, its reflect.Value, and a Set closure.
//
// A non-nil *struct yields settable fields; a value struct is read-only and
// each Field.Set returns ErrNotSettable. Anything that is not a struct or
// non-nil *struct returns ErrNotStruct. Traversal is shallow: an anonymous
// embedded struct is yielded as a single field, not flattened. Unexported
// fields are skipped. fn's error stops the walk and is returned as-is.
func Walk(v any, tagKey string, fn func(Field) error) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return ErrNotStruct
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return ErrNotStruct
	}

	rt := rv.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}

		fieldVal := rv.Field(i)
		field := Field{
			Name:  sf.Name,
			Tag:   parseTag(sf.Tag.Get(tagKey)),
			Value: fieldVal,
			Set:   makeSetter(sf.Name, fieldVal),
		}
		if err := fn(field); err != nil {
			return err
		}
	}
	return nil
}

// makeSetter returns a closure that assigns/converts v into target, or
// ErrNotSettable when target cannot be set (value-struct walk).
func makeSetter(name string, target reflect.Value) func(v any) error {
	return func(v any) error {
		if !target.CanSet() {
			return fmt.Errorf("structfields: field %q: %w", name, ErrNotSettable)
		}
		in := reflect.ValueOf(v)
		if !in.IsValid() {
			return fmt.Errorf("structfields: field %q: cannot set from nil", name)
		}
		tt := target.Type()
		switch {
		case in.Type().AssignableTo(tt):
			target.Set(in)
		case in.Type().ConvertibleTo(tt):
			target.Set(in.Convert(tt))
		default:
			return fmt.Errorf("structfields: field %q: cannot set %s from %s", name, tt, in.Type())
		}
		return nil
	}
}
```

Note on the type-mismatch test: `string` is not convertible to `int` in Go's reflect model, so `"not-a-number"` into `Age int` falls through to the default branch and returns the `cannot set` error (containing `structfields:` and `Age`). Numeric widening (`int`→`int64`) hits the `ConvertibleTo` branch and succeeds.

- [ ] **Step 4: Run tests, verify pass**

Command: `go test ./structfields/ -run 'TestWalk' -v`

Expected: `ok  	github.com/dmitrymomot/forge/structfields`

- [ ] **Step 5: Commit**

```
git add structfields/structfields.go structfields/structfields_test.go
git commit -m "feat(structfields): add Field type and Walk traversal with settable Set"
```

---

### Task 4: edge-case hardening + `doc.go`

**Files:**
- Create: `structfields/doc.go`
- Edit: `structfields/structfields_test.go` (append edge-case table tests)

**Interfaces:**
- Consumes: `Walk`, `Field`, `ErrNotStruct` (Task 3).
- Produces: package documentation; no new exported symbols.

This task pins down the reject/skip behaviour called out in the spec's "Top risks #4" — non-struct inputs, nil pointer, and the shallow embedded-struct rule — and adds the package doc. It is independently rejectable because a reviewer could accept Task 3 yet reject incorrect handling of `nil` / non-struct / embedded inputs.

- [ ] **Step 1: Write the failing test** (append to `structfields/structfields_test.go`)

```go
func TestWalk_RejectsNonStruct(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"int", 42},
		{"string", "hello"},
		{"slice", []int{1, 2}},
		{"map", map[string]int{"a": 1}},
		{"nil interface", nil},
		{"pointer to int", new(int)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := structfields.Walk(c.in, "env", func(structfields.Field) error {
				t.Fatal("callback must not run for non-struct input")
				return nil
			})
			assert.True(t, errors.Is(err, structfields.ErrNotStruct), "want ErrNotStruct for %s", c.name)
		})
	}
}

func TestWalk_RejectsNilStructPointer(t *testing.T) {
	var s *walkSample
	err := structfields.Walk(s, "env", func(structfields.Field) error {
		t.Fatal("callback must not run for nil pointer")
		return nil
	})
	assert.True(t, errors.Is(err, structfields.ErrNotStruct))
}

func TestWalk_EmptyStructVisitsNothing(t *testing.T) {
	type empty struct{}
	var count int
	err := structfields.Walk(&empty{}, "env", func(structfields.Field) error {
		count++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

type embeddedInner struct {
	Inner string `env:"INNER"`
}

type embeddedOuter struct {
	embeddedInner        // exported anonymous embedded struct
	Outer         string `env:"OUTER"`
}

func TestWalk_ShallowEmbeddedStructNotFlattened(t *testing.T) {
	var s embeddedOuter
	var names []string
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		names = append(names, f.Name)
		return nil
	})
	require.NoError(t, err)
	// Shallow: the embedded struct is a single field named after its type,
	// NOT flattened into Inner. Outer is a normal field.
	assert.Equal(t, []string{"embeddedInner", "Outer"}, names)
}

func TestWalk_EmbeddedFieldReWalkable(t *testing.T) {
	// A caller needing recursion re-Walks the embedded field's value.
	var s embeddedOuter
	var inner []string
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		if f.Name == "embeddedInner" {
			return structfields.Walk(f.Value.Addr().Interface(), "env", func(g structfields.Field) error {
				inner = append(inner, g.Name)
				return g.Set("nested")
			})
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Inner"}, inner)
	assert.Equal(t, "nested", s.Inner, "re-Walk on embedded field is settable")
}
```

- [ ] **Step 2: Run it, verify it fails**

Command: `go test ./structfields/ -run 'TestWalk_RejectsNonStruct|TestWalk_RejectsNilStructPointer|TestWalk_EmptyStructVisitsNothing|TestWalk_ShallowEmbeddedStructNotFlattened|TestWalk_EmbeddedFieldReWalkable' -v`

Expected: all five pass immediately IF the Task 3 implementation is correct — the `reflect.Pointer` + `IsNil` guards and the exported-field / declaration-order loop already satisfy them. If any FAIL, the failure message will be a concrete assertion mismatch (e.g. `Not equal: expected: []string{"embeddedInner","Outer"}`) identifying the gap to fix in `structfields.go` before proceeding. (These tests exist to lock the behaviour against regressions and document the shallow-embed contract; run this step to confirm green, then add `doc.go`.)

- [ ] **Step 3: Write the minimal implementation** — add `structfields/doc.go`

If any edge-case test failed in Step 2, first correct `structfields/structfields.go` so the guards match the assertions (nil-pointer → `ErrNotStruct`, non-struct → `ErrNotStruct`, exported-only, declaration order, shallow embed). With the tests green, add the package doc:

`structfields/doc.go`:

```go
// Package structfields is forge's single sanctioned reflection helper. Walk
// visits each exported field of a struct (or non-nil *struct) exactly once,
// handing the caller the field name, its parsed struct tag (Tag), the field's
// reflect.Value, and a Set closure — so consumers like envconfig, form binding,
// and row scanning stay reflection-free themselves by depending on this one
// audited primitive.
//
// A non-nil *struct yields settable fields (Set assigns/converts into the
// value); a value struct walks read-only (Set returns ErrNotSettable). Any
// other input returns ErrNotStruct.
//
// What this is NOT: it does not flatten anonymous embedded structs — an
// embedded struct is yielded as one field, and a caller needing recursion
// re-Walks that field's value (embedded flattening + name prefixing may be
// added later without an API break). It visits only exported fields. It does
// not bind or populate structs from external data, does not validate, and does
// not parse scalar values — struct-tag binding lives in the consumers, scalar
// conversion in typeconv, and value validation in validate.
package structfields
```

- [ ] **Step 4: Run tests, verify pass**

Command: `just test ./structfields/...`

Expected: `ok  	github.com/dmitrymomot/forge/structfields`

- [ ] **Step 5: Commit**

```
git add structfields/doc.go structfields/structfields_test.go
git commit -m "test(structfields): pin non-struct/nil/embedded edge cases; add package doc"
```

---

### Task 5: final verification — `just check` clean

**Files:**
- Edit: none expected (fix only if a linter flags something).

**Interfaces:**
- Consumes: the whole package.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test** — no new test; this task is the whole-package quality gate. (If a gap surfaces, add a black-box table test in `structfields/structfields_test.go` reproducing it before fixing.)

- [ ] **Step 2: Run it, verify it fails** — run the gate to see current state:

Command: `just check`

Expected at this point: PASS if Tasks 1–4 are clean. If `modernize`/`betteralign`/`nilaway`/`golangci-lint` flag anything (e.g. field ordering in `Field`/`Tag`, a `range int` opportunity, or a `new(expr)` suggestion), that is the "fail" to address in Step 3.

- [ ] **Step 3: Write the minimal implementation** — apply any linter-directed fixes:
  - Run `just fmt` to format.
  - Apply `modernize` suggestions (e.g. `for i := range rt.NumField()` is already modern; ensure no `ptr.To`-style wrappers exist — none should).
  - If `betteralign` reports struct-field ordering for `Field` or `Tag`, reorder fields as directed (keep exported API identical; only field order changes).
  - If `nilaway` flags a possible nil deref, add the guard it points to (the nil-pointer path already returns `ErrNotStruct` before `Elem()`).

- [ ] **Step 4: Run tests, verify pass**

Command: `just check`

Expected: clean fmt + lint + test with `-race` — final line `ok  	github.com/dmitrymomot/forge/structfields` and no golangci-lint / go vet / nilaway / betteralign / modernize findings.

- [ ] **Step 5: Commit** (only if Step 3 changed files; otherwise skip)

```
git add structfields/
git commit -m "chore(structfields): satisfy lint/align/modernize gate"
```
