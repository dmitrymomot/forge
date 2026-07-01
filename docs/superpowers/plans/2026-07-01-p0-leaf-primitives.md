# P0 Leaf Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build five stdlib-only P0 leaf packages — `typeconv`, `iox`, `bufpool`, `nullx`, `bytesize` — that fill the remaining foundation gaps below the web/data/crypto layers.

**Architecture:** Each package is an independent leaf with zero forge dependencies: stateless free-funcs and/or generic value types. No configured services, so no `New(...Option)`/`Config` and no builders. They can be built in any order or in parallel.

**Tech Stack:** Go 1.26, stdlib only (`strconv`, `time`, `strings`, `io`, `bytes`, `sync`, `database/sql`, `encoding/json`, `math`, `errors`, `fmt`).

## Global Constraints

- **Go 1.26**; use the `new(expr)` builtin over any pointer-literal wrapper; run `modernize` before done.
- **stdlib only, zero forge deps** in every package here.
- **No `unsafe`, no reflection.** (The repo uses neither; keep it that way.)
- **Black-box tests only** — test files are `package <pkg>_test` and import via `github.com/dmitrymomot/forge/<pkg>`.
- **Errors** are `errors.Is`-matchable single-line sentinels in an `errors.go`; wrap underlying causes with `fmt.Errorf("%w: …", Sentinel, …)`.
- **Anatomy:** `doc.go` (package doc comment), `errors.go` (if the package has sentinels), impl file(s). Public methods never return unexported types.
- **Verify each task** with `just check` (fmt + lint + test) from the worktree root; commit only when green. No Claude attribution in commit messages.
- **Benchmarks:** each package ships a `<pkg>_bench_test.go` (black-box) with `Benchmark*` funcs using `for b.Loop()` + `b.ReportAllocs()`. Performance-contract packages (`bufpool`, `typeconv`, `iox`) also assert allocation invariants with `testing.AllocsPerRun` in a `Test*` so `just test` enforces them. Run benchmarks with `just bench ./<pkg>/...`.
- Module path: `github.com/dmitrymomot/forge`.

---

### Task 1: `typeconv` — scalar string ⇄ Go coercion (core)

**Files:**
- Create: `typeconv/errors.go`
- Create: `typeconv/typeconv.go`
- Create: `typeconv/doc.go`
- Test: `typeconv/typeconv_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `Parse[T any](s string) (T, error)`
  - `Format(v any) string`
  - `ParseBool(s string) (bool, error)`
  - `ParseInt[T signed](s string) (T, error)`
  - `ParseUint[T unsigned](s string) (T, error)`
  - `ParseFloat[T float](s string) (T, error)`
  - `ParseDuration(s string) (time.Duration, error)`
  - `ParseTime(s string) (time.Time, error)`
  - `ParseSlice[T any](s, sep string) ([]T, error)`
  - `var ErrUnsupportedType, ErrSyntax error`

- [ ] **Step 1: Write the failing tests**

Create `typeconv/typeconv_test.go`:

```go
package typeconv_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/typeconv"
)

func TestParse(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		v, err := typeconv.Parse[string]("hello")
		if err != nil || v != "hello" {
			t.Fatalf("got %q, %v", v, err)
		}
	})
	t.Run("bool", func(t *testing.T) {
		v, err := typeconv.Parse[bool]("true")
		if err != nil || !v {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("int", func(t *testing.T) {
		v, err := typeconv.Parse[int]("42")
		if err != nil || v != 42 {
			t.Fatalf("got %d, %v", v, err)
		}
	})
	t.Run("int8 out of range", func(t *testing.T) {
		if _, err := typeconv.Parse[int8]("999"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
	t.Run("float64", func(t *testing.T) {
		v, err := typeconv.Parse[float64]("3.14")
		if err != nil || v != 3.14 {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("duration", func(t *testing.T) {
		v, err := typeconv.Parse[time.Duration]("1h30m")
		if err != nil || v != 90*time.Minute {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("time RFC3339", func(t *testing.T) {
		v, err := typeconv.Parse[time.Time]("2026-07-01T12:00:00Z")
		if err != nil || !v.Equal(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)) {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("syntax error wraps ErrSyntax", func(t *testing.T) {
		if _, err := typeconv.Parse[int]("nope"); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
	t.Run("unsupported type", func(t *testing.T) {
		if _, err := typeconv.Parse[[]byte]("x"); !errors.Is(err, typeconv.ErrUnsupportedType) {
			t.Fatalf("want ErrUnsupportedType, got %v", err)
		}
	})
}

func TestParseIntHelperOverflow(t *testing.T) {
	if _, err := typeconv.ParseInt[int8]("128"); !errors.Is(err, typeconv.ErrSyntax) {
		t.Fatalf("want overflow ErrSyntax, got %v", err)
	}
	if v, err := typeconv.ParseInt[int8]("127"); err != nil || v != 127 {
		t.Fatalf("got %d, %v", v, err)
	}
	// Defined types are handled by the constraint helpers.
	type Port uint16
	if _, err := typeconv.ParseUint[Port]("70000"); !errors.Is(err, typeconv.ErrSyntax) {
		t.Fatalf("want overflow ErrSyntax, got %v", err)
	}
	if v, err := typeconv.ParseUint[Port]("8080"); err != nil || v != 8080 {
		t.Fatalf("got %d, %v", v, err)
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"hi", "hi"},
		{true, "true"},
		{42, "42"},
		{int64(-7), "-7"},
		{uint(8), "8"},
		{3.5, "3.5"},
		{90 * time.Minute, "1h30m0s"},
		{time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), "2026-07-01T12:00:00Z"},
	}
	for _, c := range cases {
		if got := typeconv.Format(c.in); got != c.want {
			t.Errorf("Format(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSlice(t *testing.T) {
	t.Run("ints", func(t *testing.T) {
		v, err := typeconv.ParseSlice[int]("1, 2, 3", ",")
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 3 || v[0] != 1 || v[2] != 3 {
			t.Fatalf("got %v", v)
		}
	})
	t.Run("trailing sep and blanks dropped", func(t *testing.T) {
		v, err := typeconv.ParseSlice[string]("a, ,b,", ",")
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 2 || v[0] != "a" || v[1] != "b" {
			t.Fatalf("got %v", v)
		}
	})
	t.Run("empty input yields nil", func(t *testing.T) {
		v, err := typeconv.ParseSlice[int]("  ", ",")
		if err != nil || v != nil {
			t.Fatalf("got %v, %v", v, err)
		}
	})
	t.Run("bad element errors", func(t *testing.T) {
		if _, err := typeconv.ParseSlice[int]("1,x,3", ","); !errors.Is(err, typeconv.ErrSyntax) {
			t.Fatalf("want ErrSyntax, got %v", err)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/dmitrymomot/Dev/claude_worktrees/forge/nice-antonelli-0ca30d && just test ./typeconv/...`
Expected: FAIL — build error, `undefined: typeconv.Parse` etc.

- [ ] **Step 3: Write `errors.go` and `doc.go`**

Create `typeconv/errors.go`:

```go
package typeconv

import "errors"

// ErrUnsupportedType is returned by Parse when T is not one of the supported
// base kinds.
var ErrUnsupportedType = errors.New("typeconv: unsupported type")

// ErrSyntax is returned when the input cannot be parsed into the target type.
// It wraps the underlying strconv/time error.
var ErrSyntax = errors.New("typeconv: invalid syntax")
```

Create `typeconv/doc.go`:

```go
// Package typeconv converts strings to Go scalars and back without reflection.
//
// It is the scalar substrate that envconfig, form decoding, and featureflag
// build field decoders on: Parse[T] and the ParseInt/ParseUint/... helpers turn
// a string into a typed value; Format is the lossless inverse. Struct-field
// walking belongs to structfields; locale-aware parsing belongs to i18n.
//
// Parse[T] dispatches on the exact base kind, so a defined type (type Status
// string) will not match generic Parse — numeric defined types are served by
// the constraint helpers (ParseInt[MyInt]) and string-defined types by a
// trivial conversion. Time is RFC3339 both ways.
package typeconv
```

- [ ] **Step 4: Write `typeconv.go`**

Create `typeconv/typeconv.go`:

```go
package typeconv

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type float interface {
	~float32 | ~float64
}

func syntax(err error) error { return fmt.Errorf("%w: %v", ErrSyntax, err) }

// Parse converts s into T. Supported T are the base kinds string, bool, the
// sized int/uint kinds, float32/64, time.Duration, and time.Time. A defined
// type does not match; use the constraint helpers or an explicit conversion.
func Parse[T any](s string) (T, error) {
	var zero T
	var out any
	var err error
	switch any(zero).(type) {
	case string:
		out = s
	case bool:
		out, err = strconv.ParseBool(s)
	case int:
		var v int64
		v, err = strconv.ParseInt(s, 10, strconv.IntSize)
		out = int(v)
	case int8:
		var v int64
		v, err = strconv.ParseInt(s, 10, 8)
		out = int8(v)
	case int16:
		var v int64
		v, err = strconv.ParseInt(s, 10, 16)
		out = int16(v)
	case int32:
		var v int64
		v, err = strconv.ParseInt(s, 10, 32)
		out = int32(v)
	case int64:
		out, err = strconv.ParseInt(s, 10, 64)
	case uint:
		var v uint64
		v, err = strconv.ParseUint(s, 10, strconv.IntSize)
		out = uint(v)
	case uint8:
		var v uint64
		v, err = strconv.ParseUint(s, 10, 8)
		out = uint8(v)
	case uint16:
		var v uint64
		v, err = strconv.ParseUint(s, 10, 16)
		out = uint16(v)
	case uint32:
		var v uint64
		v, err = strconv.ParseUint(s, 10, 32)
		out = uint32(v)
	case uint64:
		out, err = strconv.ParseUint(s, 10, 64)
	case float32:
		var v float64
		v, err = strconv.ParseFloat(s, 32)
		out = float32(v)
	case float64:
		out, err = strconv.ParseFloat(s, 64)
	case time.Duration:
		out, err = time.ParseDuration(s)
	case time.Time:
		out, err = time.Parse(time.RFC3339, s)
	default:
		return zero, fmt.Errorf("%w: %T", ErrUnsupportedType, zero)
	}
	if err != nil {
		return zero, syntax(err)
	}
	return out.(T), nil
}

// Format is the lossless inverse of Parse for the supported scalar set. Any
// other type is rendered with fmt.Sprint.
func Format(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case uintptr:
		return strconv.FormatUint(uint64(x), 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Duration:
		return x.String()
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprint(v)
	}
}

// ParseBool parses a boolean via strconv.ParseBool.
func ParseBool(s string) (bool, error) {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, syntax(err)
	}
	return v, nil
}

// ParseInt parses a signed integer of any width into T, rejecting values that
// overflow T (detected by a narrowing round-trip).
func ParseInt[T signed](s string) (T, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, syntax(err)
	}
	r := T(v)
	if int64(r) != v {
		return 0, fmt.Errorf("%w: %d overflows target type", ErrSyntax, v)
	}
	return r, nil
}

// ParseUint parses an unsigned integer of any width into T, rejecting overflow.
func ParseUint[T unsigned](s string) (T, error) {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, syntax(err)
	}
	r := T(v)
	if uint64(r) != v {
		return 0, fmt.Errorf("%w: %d overflows target type", ErrSyntax, v)
	}
	return r, nil
}

// ParseFloat parses a float into T. float32 out-of-range parses to ±Inf per
// strconv; width range is best-effort.
func ParseFloat[T float](s string) (T, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, syntax(err)
	}
	return T(v), nil
}

// ParseDuration parses a Go duration string ("1h30m").
func ParseDuration(s string) (time.Duration, error) {
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, syntax(err)
	}
	return v, nil
}

// ParseTime parses an RFC3339 timestamp.
func ParseTime(s string) (time.Time, error) {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, syntax(err)
	}
	return v, nil
}

// ParseSlice splits s on sep and Parse[T]s each element. It trims whitespace
// around the whole input and each element, drops empty-after-trim elements
// (so "1, 2, 3," yields [1 2 3]), and returns nil for empty input.
func ParseSlice[T any](s, sep string) ([]T, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, sep)
	out := make([]T, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := Parse[T](p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests + lint to verify they pass**

Run: `just test ./typeconv/... && just lint`
Expected: PASS; lint clean.

- [ ] **Step 6: Write benchmarks and allocation invariants**

Create `typeconv/typeconv_bench_test.go`:

```go
package typeconv_test

import (
	"testing"

	"github.com/dmitrymomot/forge/typeconv"
)

func BenchmarkParseInt(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = typeconv.ParseInt[int]("2147483")
	}
}

func BenchmarkParseGeneric(b *testing.B) {
	// Parse boxes the result into any: small ints avoid the alloc, larger
	// values box onto the heap — prefer the typed helpers on hot paths.
	b.ReportAllocs()
	for b.Loop() {
		_, _ = typeconv.Parse[int]("2147483")
	}
}

func BenchmarkFormat(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = typeconv.Format(2147483)
	}
}

func BenchmarkParseSlice(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = typeconv.ParseSlice[int]("1,2,3,4,5", ",")
	}
}

func TestParseIntZeroAlloc(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		_, _ = typeconv.ParseInt[int]("2147483")
	}); n != 0 {
		t.Fatalf("ParseInt allocs = %v, want 0", n)
	}
}
```

- [ ] **Step 7: Run benchmarks and allocation checks**

Run: `just test ./typeconv/... && just bench ./typeconv/...`
Expected: `TestParseIntZeroAlloc` PASS; benchmarks report (BenchmarkParseGeneric may read 0 allocs for this small value but allocates for large ones — that is the point of contrasting it with BenchmarkParseInt).

- [ ] **Step 8: Commit**

```bash
git add typeconv/
git commit -m "feat(typeconv): reflection-free scalar string<->Go coercion (Parse/Format/ParseSlice)"
```

---

### Task 2: `iox` — io shims (recommended)

**Files:**
- Create: `iox/errors.go`
- Create: `iox/iox.go`
- Create: `iox/doc.go`
- Test: `iox/iox_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `LimitReader(r io.Reader, n int64) io.Reader`
  - `DrainClose(rc io.ReadCloser) error`
  - `MultiCloser(closers ...io.Closer) io.Closer`
  - `type CountingWriter struct{...}`; `NewCountingWriter(w io.Writer) *CountingWriter`; `(*CountingWriter).Write`; `(*CountingWriter).N() int64`
  - `NopWriteCloser(w io.Writer) io.WriteCloser`
  - `var ErrLimitExceeded error`

- [ ] **Step 1: Write the failing tests**

Create `iox/iox_test.go`:

```go
package iox_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/iox"
)

func TestLimitReaderUnderAndAtLimit(t *testing.T) {
	for _, limit := range []int64{5, 10} {
		r := iox.LimitReader(strings.NewReader("hello"), limit)
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("limit %d: unexpected err %v", limit, err)
		}
		if string(b) != "hello" {
			t.Fatalf("limit %d: got %q", limit, b)
		}
	}
}

func TestLimitReaderOverLimit(t *testing.T) {
	r := iox.LimitReader(strings.NewReader("hello world"), 5)
	b, err := io.ReadAll(r)
	if !errors.Is(err, iox.ErrLimitExceeded) {
		t.Fatalf("want ErrLimitExceeded, got %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("want first 5 bytes, got %q", b)
	}
}

func TestDrainClose(t *testing.T) {
	rc := io.NopCloser(strings.NewReader("data"))
	if err := iox.DrainClose(rc); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

type errCloser struct{ err error }

func (e errCloser) Close() error { return e.err }

func TestMultiCloserJoinsErrorsAndSkipsNil(t *testing.T) {
	boom := errors.New("boom")
	c := iox.MultiCloser(errCloser{boom}, errCloser{nil}, nil)
	if err := c.Close(); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestCountingWriter(t *testing.T) {
	var buf bytes.Buffer
	cw := iox.NewCountingWriter(&buf)
	if _, err := cw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := cw.Write([]byte("!")); err != nil {
		t.Fatal(err)
	}
	if cw.N() != 6 {
		t.Fatalf("N = %d, want 6", cw.N())
	}
	if buf.String() != "hello!" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestNopWriteCloser(t *testing.T) {
	var buf bytes.Buffer
	wc := iox.NopWriteCloser(&buf)
	if _, err := wc.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if buf.String() != "x" {
		t.Fatalf("got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./iox/...`
Expected: FAIL — `undefined: iox.LimitReader` etc.

- [ ] **Step 3: Write `errors.go` and `doc.go`**

Create `iox/errors.go`:

```go
package iox

import "errors"

// ErrLimitExceeded is returned by a LimitReader once the input exceeds the
// configured limit.
var ErrLimitExceeded = errors.New("iox: read limit exceeded")
```

Create `iox/doc.go`:

```go
// Package iox provides small io.Reader/Writer/Closer shims that streaming code
// reuses: a LimitReader that errors (rather than silently truncates) past its
// cap so callers can answer 413, a Drain+Close for keep-alive reuse, a
// MultiCloser, a CountingWriter, and a NopWriteCloser. It does not duplicate
// bufio or re-wrap stdlib TeeReader/io.NopCloser.
package iox
```

- [ ] **Step 4: Write `iox.go`**

Create `iox/iox.go`:

```go
package iox

import (
	"errors"
	"io"
)

// LimitReader returns a reader that yields at most n bytes from r and then
// returns ErrLimitExceeded. Unlike io.LimitReader (which reports io.EOF at the
// cap), it distinguishes "hit the limit" from "clean end of input".
func LimitReader(r io.Reader, n int64) io.Reader {
	return &limitReader{r: r, n: n}
}

type limitReader struct {
	r   io.Reader
	n   int64 // bytes still allowed
	err error
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	// Read at most one byte past the limit so we can detect an overrun.
	if int64(len(p)) > l.n+1 {
		p = p[:l.n+1]
	}
	n, err := l.r.Read(p)
	if int64(n) <= l.n {
		l.n -= int64(n)
		l.err = err
		return n, err
	}
	// Underlying reader produced a byte beyond the limit.
	l.err = ErrLimitExceeded
	return int(l.n), l.err
}

// DrainClose discards any remaining bytes then closes rc, letting an HTTP
// client reuse the keep-alive connection. It returns the copy error if any,
// otherwise the close error.
func DrainClose(rc io.ReadCloser) error {
	_, copyErr := io.Copy(io.Discard, rc)
	closeErr := rc.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// MultiCloser returns an io.Closer that closes every closer (skipping nils),
// aggregating failures with errors.Join.
func MultiCloser(closers ...io.Closer) io.Closer {
	return multiCloser(closers)
}

type multiCloser []io.Closer

func (m multiCloser) Close() error {
	var errs []error
	for _, c := range m {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// CountingWriter wraps an io.Writer and counts the bytes written through it.
type CountingWriter struct {
	w io.Writer
	n int64
}

// NewCountingWriter returns a CountingWriter delegating to w.
func NewCountingWriter(w io.Writer) *CountingWriter {
	return &CountingWriter{w: w}
}

func (c *CountingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// N returns the total number of bytes written so far.
func (c *CountingWriter) N() int64 { return c.n }

// NopWriteCloser wraps w with a no-op Close.
func NopWriteCloser(w io.Writer) io.WriteCloser {
	return nopWriteCloser{w}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
```

- [ ] **Step 5: Run tests + lint**

Run: `just test ./iox/... && just lint`
Expected: PASS; lint clean.

- [ ] **Step 6: Write benchmarks and allocation invariants**

Create `iox/iox_bench_test.go`:

```go
package iox_test

import (
	"io"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/iox"
)

func BenchmarkLimitReaderRead(b *testing.B) {
	src := strings.NewReader(strings.Repeat("x", 4096))
	buf := make([]byte, 4096)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = src.Seek(0, io.SeekStart)
		r := iox.LimitReader(src, 4096)
		_, _ = io.ReadFull(r, buf)
	}
}

func BenchmarkCountingWriter(b *testing.B) {
	cw := iox.NewCountingWriter(io.Discard)
	p := []byte("hello world")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = cw.Write(p)
	}
}

func TestCountingWriterZeroAlloc(t *testing.T) {
	cw := iox.NewCountingWriter(io.Discard)
	p := []byte("hello world")
	if n := testing.AllocsPerRun(100, func() {
		_, _ = cw.Write(p)
	}); n != 0 {
		t.Fatalf("CountingWriter.Write allocs = %v, want 0", n)
	}
}
```

- [ ] **Step 7: Run benchmarks and allocation checks**

Run: `just test ./iox/... && just bench ./iox/...`
Expected: `TestCountingWriterZeroAlloc` PASS; benchmarks report (BenchmarkLimitReaderRead includes the per-request reader construction, ~1 alloc/op).

- [ ] **Step 8: Commit**

```bash
git add iox/
git commit -m "feat(iox): limit/drain/multiclose/counting io shims"
```

---

### Task 3: `bufpool` — shared `*bytes.Buffer` pool (recommended)

**Files:**
- Create: `bufpool/bufpool.go`
- Create: `bufpool/doc.go`
- Test: `bufpool/bufpool_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `Get() *bytes.Buffer`
  - `Put(b *bytes.Buffer)`
  - `Do(fn func(*bytes.Buffer) error) error`

- [ ] **Step 1: Write the failing tests**

Create `bufpool/bufpool_test.go`:

```go
package bufpool_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/bufpool"
)

func TestGetReturnsResetBuffer(t *testing.T) {
	b := bufpool.Get()
	if b.Len() != 0 {
		t.Fatalf("want empty, got len %d", b.Len())
	}
	b.WriteString("data")
	bufpool.Put(b)
	b2 := bufpool.Get()
	if b2.Len() != 0 {
		t.Fatalf("want reset buffer, got len %d", b2.Len())
	}
	bufpool.Put(b2)
}

func TestPutNilNoPanic(t *testing.T) {
	bufpool.Put(nil)
}

func TestDoReturnsValue(t *testing.T) {
	sentinel := errors.New("bad")
	err := bufpool.Do(func(b *bytes.Buffer) error {
		b.WriteString("hi")
		if b.String() != "hi" {
			return sentinel
		}
		return nil
	})
	if err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestDoPropagatesPanicAndPoolStaysUsable(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic to propagate")
		}
		b := bufpool.Get()
		if b.Len() != 0 {
			t.Fatal("pool corrupted after panic")
		}
		bufpool.Put(b)
	}()
	_ = bufpool.Do(func(b *bytes.Buffer) error {
		panic("boom")
	})
}

func TestConcurrentRace(t *testing.T) {
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := bufpool.Get()
			b.WriteString("x")
			bufpool.Put(b)
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./bufpool/...`
Expected: FAIL — `undefined: bufpool.Get` etc.

- [ ] **Step 3: Write `doc.go` and `bufpool.go`**

Create `bufpool/doc.go`:

```go
// Package bufpool is one shared, size-capped sync.Pool of *bytes.Buffer so
// transactional renderers and encoders stop each defining a private getBuf/
// putBuf. It is deliberately zero-config: the retained-capacity cap is a tuned
// constant, which is what distinguishes it from a generic pool[T].
package bufpool
```

Create `bufpool/bufpool.go`:

```go
package bufpool

import (
	"bytes"
	"sync"
)

// maxCap bounds the capacity of buffers retained by the pool. Larger buffers
// are dropped on Put so a single big render cannot pin memory in the pool.
const maxCap = 64 << 10 // 64 KiB

var pool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Get returns a reset buffer from the shared pool.
func Get() *bytes.Buffer {
	b := pool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

// Put returns b to the pool. A nil buffer, or one whose capacity exceeds the
// internal cap, is dropped rather than retained.
func Put(b *bytes.Buffer) {
	if b == nil || b.Cap() > maxCap {
		return
	}
	pool.Put(b)
}

// Do borrows a buffer, passes it to fn, and returns it to the pool afterwards —
// even if fn panics. The buffer must not be retained after fn returns.
func Do(fn func(*bytes.Buffer) error) error {
	b := Get()
	defer Put(b)
	return fn(b)
}
```

- [ ] **Step 4: Run tests (with race) + lint**

Run: `just test ./bufpool/... && go test -race ./bufpool/... && just lint`
Expected: PASS; no race; lint clean.

- [ ] **Step 5: Write benchmarks and allocation invariants**

Create `bufpool/bufpool_bench_test.go`:

```go
package bufpool_test

import (
	"bytes"
	"testing"

	"github.com/dmitrymomot/forge/bufpool"
)

func BenchmarkGetPut(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		buf := bufpool.Get()
		buf.WriteString("hello world")
		bufpool.Put(buf)
	}
}

func BenchmarkDo(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = bufpool.Do(func(buf *bytes.Buffer) error {
			buf.WriteString("hello world")
			return nil
		})
	}
}

func TestGetPutZeroAlloc(t *testing.T) {
	// Warm the pool, then steady-state reuse must not allocate. If this proves
	// platform-flaky under GC pressure, relax to n > 1 (mirroring the id
	// package's <=1 alloc precedent) rather than deleting the guard.
	if n := testing.AllocsPerRun(1000, func() {
		buf := bufpool.Get()
		buf.WriteString("hello world")
		bufpool.Put(buf)
	}); n != 0 {
		t.Fatalf("Get/Put steady-state allocs = %v, want 0", n)
	}
}
```

- [ ] **Step 6: Run benchmarks and allocation checks**

Run: `just test ./bufpool/... && just bench ./bufpool/...`
Expected: `TestGetPutZeroAlloc` PASS; BenchmarkGetPut reports 0 allocs/op on the steady state.

- [ ] **Step 7: Commit**

```bash
git add bufpool/
git commit -m "feat(bufpool): shared size-capped *bytes.Buffer pool"
```

---

### Task 4: `nullx` — `Null[T]` for SQL + JSON null (recommended)

**Files:**
- Create: `nullx/nullx.go`
- Create: `nullx/doc.go`
- Test: `nullx/nullx_test.go`

**Interfaces:**
- Consumes: nothing (leaf; wraps stdlib `sql.Null[T]`).
- Produces:
  - `type Null[T any] struct{ sql.Null[T] }`
  - `Of[T any](v T) Null[T]`
  - `Empty[T any]() Null[T]`
  - `(Null[T]).Get() (T, bool)`
  - `(Null[T]).Ptr() *T`
  - `FromPtr[T any](p *T) Null[T]`
  - `(Null[T]).MarshalJSON() ([]byte, error)`
  - `(*Null[T]).UnmarshalJSON([]byte) error`

- [ ] **Step 1: Write the failing tests**

Create `nullx/nullx_test.go`:

```go
package nullx_test

import (
	"encoding/json"
	"testing"

	"github.com/dmitrymomot/forge/nullx"
)

func TestOfEmptyGet(t *testing.T) {
	if v, ok := nullx.Of("hi").Get(); !ok || v != "hi" {
		t.Fatalf("got %q ok=%v", v, ok)
	}
	if _, ok := nullx.Empty[string]().Get(); ok {
		t.Fatal("want invalid")
	}
}

func TestPtrAndFromPtr(t *testing.T) {
	if nullx.Empty[int]().Ptr() != nil {
		t.Fatal("want nil Ptr")
	}
	p := nullx.Of(9).Ptr()
	if p == nil || *p != 9 {
		t.Fatalf("got %v", p)
	}
	*p = 100 // must be a copy; mutating it affects nothing else

	x := 3
	if v, ok := nullx.FromPtr(&x).Get(); !ok || v != 3 {
		t.Fatalf("got %d ok=%v", v, ok)
	}
	if _, ok := nullx.FromPtr[int](nil).Get(); ok {
		t.Fatal("want invalid for nil pointer")
	}
}

func TestJSON(t *testing.T) {
	b, err := json.Marshal(nullx.Of(5))
	if err != nil || string(b) != "5" {
		t.Fatalf("got %s, %v", b, err)
	}
	b, err = json.Marshal(nullx.Empty[int]())
	if err != nil || string(b) != "null" {
		t.Fatalf("got %s, %v", b, err)
	}

	var n nullx.Null[int]
	if err := json.Unmarshal([]byte("null"), &n); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.Get(); ok {
		t.Fatal("want invalid after null")
	}
	if err := json.Unmarshal([]byte("7"), &n); err != nil {
		t.Fatal(err)
	}
	if v, ok := n.Get(); !ok || v != 7 {
		t.Fatalf("got %d ok=%v", v, ok)
	}
}

func TestSQLRoundTrip(t *testing.T) {
	n := nullx.Of[string]("hi")
	v, err := n.Value()
	if err != nil {
		t.Fatal(err)
	}
	var m nullx.Null[string]
	if err := m.Scan(v); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Get(); !ok || got != "hi" {
		t.Fatalf("got %q ok=%v", got, ok)
	}

	e := nullx.Empty[int64]()
	ev, err := e.Value()
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Fatalf("want nil driver value, got %v", ev)
	}
	var me nullx.Null[int64]
	if err := me.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := me.Get(); ok {
		t.Fatal("want invalid after Scan(nil)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./nullx/...`
Expected: FAIL — `undefined: nullx.Of` etc.

- [ ] **Step 3: Write `doc.go` and `nullx.go`**

Create `nullx/doc.go`:

```go
// Package nullx provides Null[T], a single generic nullable that round-trips
// through database/sql (via the embedded sql.Null[T]) and encoding/json
// (marshaling as JSON null when not valid), replacing the sql.NullString /
// NullInt64 family.
//
// T must be a type database/sql's conversion supports (the scalar kinds,
// time.Time, []byte, string). A JSON-column nullable over an arbitrary struct
// needs its own sql.Scanner and is out of scope. Null[T] models a SQL/JSON null
// value; ptr.Optional models JSON PATCH absence — they do not overlap.
package nullx
```

Create `nullx/nullx.go`:

```go
package nullx

import (
	"bytes"
	"database/sql"
	"encoding/json"
)

// Null is a nullable value of type T. It embeds sql.Null[T] for Scan/Value and
// adds JSON marshaling as null when not valid.
type Null[T any] struct {
	sql.Null[T]
}

// Of returns a valid Null carrying v.
func Of[T any](v T) Null[T] {
	return Null[T]{sql.Null[T]{V: v, Valid: true}}
}

// Empty returns an invalid (null) Null.
func Empty[T any]() Null[T] {
	return Null[T]{}
}

// Get returns the value and whether it is valid (non-null).
func (n Null[T]) Get() (T, bool) {
	return n.V, n.Valid
}

// Ptr returns a pointer to a copy of the value, or nil when not valid.
func (n Null[T]) Ptr() *T {
	if !n.Valid {
		return nil
	}
	v := n.V
	return &v
}

// FromPtr returns Empty when p is nil, otherwise Of(*p).
func FromPtr[T any](p *T) Null[T] {
	if p == nil {
		return Empty[T]()
	}
	return Of(*p)
}

// MarshalJSON encodes the value, or JSON null when not valid.
func (n Null[T]) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.V)
}

// UnmarshalJSON decodes JSON null as an invalid Null; any other value sets the
// value and marks it valid.
func (n *Null[T]) UnmarshalJSON(b []byte) error {
	if string(bytes.TrimSpace(b)) == "null" {
		var zero T
		n.V, n.Valid = zero, false
		return nil
	}
	if err := json.Unmarshal(b, &n.V); err != nil {
		return err
	}
	n.Valid = true
	return nil
}
```

- [ ] **Step 4: Run tests + lint**

Run: `just test ./nullx/... && just lint`
Expected: PASS; lint clean.

- [ ] **Step 5: Write benchmarks**

Create `nullx/nullx_bench_test.go`:

```go
package nullx_test

import (
	"encoding/json"
	"testing"

	"github.com/dmitrymomot/forge/nullx"
)

func BenchmarkMarshalJSON(b *testing.B) {
	n := nullx.Of("hello")
	b.ReportAllocs()
	for b.Loop() {
		_, _ = json.Marshal(n)
	}
}

func BenchmarkGet(b *testing.B) {
	n := nullx.Of(42)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = n.Get()
	}
}
```

- [ ] **Step 6: Run benchmarks**

Run: `just bench ./nullx/...`
Expected: benchmarks report (JSON marshaling allocates inherently; no zero-alloc invariant is asserted for this package).

- [ ] **Step 7: Commit**

```bash
git add nullx/
git commit -m "feat(nullx): generic Null[T] over sql.Null[T] with JSON-null marshaling"
```

---

### Task 5: `bytesize` — human byte sizes (recommended)

**Files:**
- Create: `bytesize/errors.go`
- Create: `bytesize/bytesize.go`
- Create: `bytesize/doc.go`
- Test: `bytesize/bytesize_test.go`

**Interfaces:**
- Consumes: nothing (leaf).
- Produces:
  - `type ByteSize int64`
  - consts `B, KB, MB, GB, TB, PB, KiB, MiB, GiB, TiB, PiB ByteSize`
  - `Parse(s string) (ByteSize, error)`
  - `(ByteSize).String() string`
  - `FormatSI(b ByteSize) string`; `FormatIEC(b ByteSize) string`
  - `(ByteSize).MarshalText() ([]byte, error)`; `(*ByteSize).UnmarshalText([]byte) error`
  - `var ErrInvalidSize error`

- [ ] **Step 1: Write the failing tests**

Create `bytesize/bytesize_test.go`:

```go
package bytesize_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/bytesize"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want bytesize.ByteSize
	}{
		{"512", 512},
		{"512B", 512},
		{"10MB", 10 * bytesize.MB},
		{"10 MB", 10 * bytesize.MB},
		{"10mb", 10 * bytesize.MB},
		{"10MiB", 10 * bytesize.MiB},
		{"1.5GiB", bytesize.ByteSize(1.5 * float64(bytesize.GiB))},
		{"2GB", 2 * bytesize.GB},
	}
	for _, c := range cases {
		got, err := bytesize.Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Parse(%q) = %d, want %d", c.in, int64(got), int64(c.want))
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"", "abc", "10XB", "MB", "1.2.3KB"} {
		if _, err := bytesize.Parse(in); !errors.Is(err, bytesize.ErrInvalidSize) {
			t.Errorf("Parse(%q): want ErrInvalidSize, got %v", in, err)
		}
	}
}

func TestStringRoundTrip(t *testing.T) {
	vals := []bytesize.ByteSize{
		0, 512, 1023, 1024, 1536, 1537,
		10 * bytesize.MiB, 2 * bytesize.GiB, 10 * bytesize.MB,
	}
	for _, v := range vals {
		s := v.String()
		got, err := bytesize.Parse(s)
		if err != nil {
			t.Errorf("Parse(String(%d)=%q) error: %v", int64(v), s, err)
			continue
		}
		if got != v {
			t.Errorf("round-trip %d -> %q -> %d", int64(v), s, int64(got))
		}
	}
}

func TestFormatFamilies(t *testing.T) {
	if got := bytesize.FormatIEC(1536); got != "1.5KiB" {
		t.Errorf("FormatIEC(1536) = %q", got)
	}
	if got := bytesize.FormatSI(1_500_000); got != "1.5MB" {
		t.Errorf("FormatSI(1_500_000) = %q", got)
	}
	if got := bytesize.FormatIEC(1537); got != "1537B" {
		t.Errorf("FormatIEC(1537) = %q", got)
	}
	if got := bytesize.FormatIEC(1024); got != "1KiB" {
		t.Errorf("FormatIEC(1024) = %q", got)
	}
}

func TestTextMarshaling(t *testing.T) {
	type cfg struct {
		Max bytesize.ByteSize `json:"max"`
	}
	var c cfg
	if err := json.Unmarshal([]byte(`{"max":"10MiB"}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Max != 10*bytesize.MiB {
		t.Fatalf("got %d", int64(c.Max))
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"max":"10MiB"}` {
		t.Fatalf("got %s", b)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./bytesize/...`
Expected: FAIL — `undefined: bytesize.Parse` etc.

- [ ] **Step 3: Write `errors.go` and `doc.go`**

Create `bytesize/errors.go`:

```go
package bytesize

import "errors"

// ErrInvalidSize is returned when a string cannot be parsed as a byte size.
var ErrInvalidSize = errors.New("bytesize: invalid size")
```

Create `bytesize/doc.go`:

```go
// Package bytesize parses and formats human byte sizes and provides a ByteSize
// type that drops into env-tagged config and JSON via TextMarshaler. SI
// suffixes (KB/MB/GB...) are powers of 1000; IEC suffixes (KiB/MiB/GiB...) are
// powers of 1024. Formatting defaults to IEC and always round-trips through
// Parse (values not exact in any unit fall back to a byte count). No bit units.
package bytesize
```

- [ ] **Step 4: Write `bytesize.go`**

Create `bytesize/bytesize.go`:

```go
package bytesize

import (
	"fmt"
	"strconv"
	"strings"
)

// ByteSize is a number of bytes.
type ByteSize int64

// SI (powers of 1000) and IEC (powers of 1024) unit multipliers.
const (
	B   ByteSize = 1
	KB  ByteSize = 1000
	MB  ByteSize = 1000 * KB
	GB  ByteSize = 1000 * MB
	TB  ByteSize = 1000 * GB
	PB  ByteSize = 1000 * TB
	KiB ByteSize = 1024
	MiB ByteSize = 1024 * KiB
	GiB ByteSize = 1024 * MiB
	TiB ByteSize = 1024 * GiB
	PiB ByteSize = 1024 * TiB
)

// parseUnits are matched against the uppercased input, longest first so IEC
// suffixes win before their SI prefixes and the bare "B".
var parseUnits = []struct {
	suffix string
	mult   ByteSize
}{
	{"PIB", PiB}, {"TIB", TiB}, {"GIB", GiB}, {"MIB", MiB}, {"KIB", KiB},
	{"PB", PB}, {"TB", TB}, {"GB", GB}, {"MB", MB}, {"KB", KB},
	{"B", B},
}

// Parse converts a human byte-size string ("10MB", "1.5GiB", "512", "10 MB")
// into a ByteSize. Suffixes are case-insensitive; a bare number is bytes.
func Parse(s string) (ByteSize, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, s)
	}
	upper := strings.ToUpper(trimmed)
	for _, u := range parseUnits {
		if strings.HasSuffix(upper, u.suffix) {
			num := strings.TrimSpace(upper[:len(upper)-len(u.suffix)])
			return parseNumber(num, u.mult, s)
		}
	}
	return parseNumber(upper, B, s)
}

func parseNumber(num string, mult ByteSize, orig string) (ByteSize, error) {
	if num == "" {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, orig)
	}
	if i, err := strconv.ParseInt(num, 10, 64); err == nil {
		return ByteSize(i) * mult, nil
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidSize, orig)
	}
	return ByteSize(f * float64(mult)), nil
}

// String formats b using IEC units (the default for infra config).
func (b ByteSize) String() string { return FormatIEC(b) }

// FormatIEC formats b with binary (1024) units: B, KiB, MiB, GiB, TiB, PiB.
func FormatIEC(b ByteSize) string {
	return format(int64(b), 1024, []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"})
}

// FormatSI formats b with decimal (1000) units: B, KB, MB, GB, TB, PB.
func FormatSI(b ByteSize) string {
	return format(int64(b), 1000, []string{"B", "KB", "MB", "GB", "TB", "PB"})
}

// format renders n in the largest unit in which it is exact to at most two
// decimals, falling back to a byte count so the output always round-trips
// through Parse. rem*100 cannot overflow int64: rem < base^i <= 1024^5.
func format(n int64, base int64, units []string) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	// Climb to the largest unit whose multiplier does not exceed n.
	pow := int64(1)
	i := 0
	for i+1 < len(units) && n >= pow*base {
		pow *= base
		i++
	}
	// Step back down until the value is exact to at most two decimals.
	for i > 0 {
		rem := n % pow
		if rem == 0 || (rem*100)%pow == 0 {
			break
		}
		pow /= base
		i--
	}
	if i == 0 {
		return sign + strconv.FormatInt(n, 10) + units[0]
	}
	whole := n / pow
	rem := n % pow
	if rem == 0 {
		return sign + strconv.FormatInt(whole, 10) + units[i]
	}
	frac := (rem * 100) / pow // 0..99, exact by the loop's invariant
	s := fmt.Sprintf("%d.%02d", whole, frac)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return sign + s + units[i]
}

// MarshalText renders b as its String form.
func (b ByteSize) MarshalText() ([]byte, error) {
	return []byte(b.String()), nil
}

// UnmarshalText parses p into b.
func (b *ByteSize) UnmarshalText(p []byte) error {
	v, err := Parse(string(p))
	if err != nil {
		return err
	}
	*b = v
	return nil
}
```

- [ ] **Step 5: Run tests + lint**

Run: `just test ./bytesize/... && just lint`
Expected: PASS (including the 1537→"1537B" byte-count fallback round-trip); lint clean.

- [ ] **Step 6: Write benchmarks**

Create `bytesize/bytesize_bench_test.go`:

```go
package bytesize_test

import (
	"testing"

	"github.com/dmitrymomot/forge/bytesize"
)

func BenchmarkParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = bytesize.Parse("1.5GiB")
	}
}

func BenchmarkString(b *testing.B) {
	v := 10 * bytesize.MiB
	b.ReportAllocs()
	for b.Loop() {
		_ = v.String()
	}
}
```

- [ ] **Step 7: Run benchmarks**

Run: `just bench ./bytesize/...`
Expected: benchmarks report (config-load path; no zero-alloc invariant is asserted for this package).

- [ ] **Step 8: Commit**

```bash
git add bytesize/
git commit -m "feat(bytesize): SI/IEC byte-size parse/format + ByteSize config type"
```

---

## Final verification

- [ ] Run the full suite and lint once more: `just check` (this runs the `AllocsPerRun` invariant tests for `typeconv`/`iox`/`bufpool`).
- [ ] Run all benchmarks once for a sanity sweep: `just bench ./...` (no panics; allocation numbers look sane).
- [ ] Confirm all five packages are present and each committed separately.

## Self-review notes (author)

- **Spec coverage:** typeconv (Parse/Format/ParseBool/Int/Uint/Float/Duration/Time/ParseSlice + sentinels) → Task 1; iox (LimitReader/DrainClose/MultiCloser/CountingWriter/NopWriteCloser + ErrLimitExceeded) → Task 2; bufpool (Get/Put/Do + 64KiB cap) → Task 3; nullx (Null[T] wrapping sql.Null[T] + Of/Empty/Get/Ptr/FromPtr + JSON) → Task 4; bytesize (ByteSize + consts + Parse/String/FormatSI/FormatIEC/Text marshaling + ErrInvalidSize) → Task 5. All spec items mapped.
- **Deviation from spec, deliberate:** width-correct generic int parsing uses a narrowing round-trip check (`int64(T(v)) != v`) instead of `unsafe.Sizeof`, because the repo uses no `unsafe`. Same observable behavior (overflow → ErrSyntax), reflection-free, handles defined types.
- **bytesize round-trip:** the exact-or-byte-count `format` makes String→Parse total (the `1537 → "1537B"` case in the test proves the fallback), satisfying spec risk #3.
- **cap-drop (bufpool):** the 64 KiB drop is not directly black-box observable (sync.Pool is nondeterministic); it is covered by the constant + code review, not a dedicated test. Documented here so it is not mistaken for missing coverage.
- **benchmarks:** all five packages ship `Benchmark*` suites; `bufpool`/`typeconv`/`iox` additionally lock zero-alloc contracts via `testing.AllocsPerRun` `Test*`s enforced by `just check`. `nullx`/`bytesize` get benchmarks only (JSON and the config-load path allocate; no hard invariant).
