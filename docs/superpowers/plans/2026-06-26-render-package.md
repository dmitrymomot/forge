# Render Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a stdlib-only `render` package of stateless free functions that write HTTP responses (JSON/JSONStream, HTML, templ, Text, Blob, CSV, Stream, Attachment, File/FileFS, Redirect, NoContent).

**Architecture:** Plain free functions over `http.ResponseWriter`. `JSON`/`HTML`/`Templ` are *transactional* — they encode into a pooled `bytes.Buffer` first, so an encode error returns with nothing written. The streaming writers (`JSONStream`, `CSV`, `Stream`, `Attachment`) write straight to the wire. templ is supported via a locally-declared structural `Component` interface, so the package takes **no** dependency on `github.com/a-h/templ`.

**Tech Stack:** Go 1.26, stdlib only (`encoding/json`, `encoding/csv`, `html/template`, `net/http`, `io`, `io/fs`, `context`, `bytes`, `sync`, `strings`, `fmt`, `errors`); `testify` in tests only.

**Spec:** `docs/superpowers/specs/2026-06-26-render-package-design.md`

## Global Constraints

- **Module:** `github.com/dmitrymomot/forge`; new package dir `render/`, package `render`, import path `github.com/dmitrymomot/forge/render`. Flat top-level layout (no nested dirs).
- **Production code is stdlib-only.** No external dependencies. No `github.com/a-h/templ` import — templ is supported structurally.
- **Tests are black-box:** every `*_test.go` is `package render_test` and imports the package; only the exported surface is exercised. `testify` (`assert`/`require`) only.
- **Errors are single-line**, lowercase, `render:`-prefixed, wrapped with `%w` (e.g. `fmt.Errorf("render: encode json: %w", err)`). No multi-line blobs in error strings.
- **`Content-Type` is set-if-empty** — only when the caller has not already set one.
- **Lint must stay green.** `just lint` runs `go vet`, `go build`, `golangci-lint` (standard + misspell + unconvert; `unused` flags unused *unexported* identifiers — add each unexported const only in the task that first uses it), `nilaway`, `betteralign`, and `modernize`. Write modern idioms: `any` (not `interface{}`), `for i := range len(s)` (not C-style loops). `getBuf` uses a checked type assertion so it never returns nil.
- **Verify with `just`:** per-task `just test ./render/` (`go test -race -cover`); final task runs full `just check` (`fmt` + `lint` + `test`).
- Commit after every task (work on `main`, per CLAUDE.md).

---

### Task 1: JSON & JSONStream (+ internal foundation)

**Files:**
- Create: `render/content.go` (internal: `contentTypeJSON`, `setContentType`, buffer pool)
- Create: `render/json.go`
- Test: `render/json_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `func JSON(w http.ResponseWriter, status int, v any) error`
  - `func JSONStream(w http.ResponseWriter, status int, v any) error`
  - internal (for later tasks): `func setContentType(w http.ResponseWriter, ct string)`, `func getBuf() *bytes.Buffer`, `func putBuf(b *bytes.Buffer)`, `const contentTypeJSON`

- [ ] **Step 1: Write the failing test** — create `render/json_test.go`:

```go
package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestJSON_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSON(rec, http.StatusCreated, map[string]int{"n": 1})
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"n":1}`, rec.Body.String())
}

func TestJSON_TransactionalFailureWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSON(rec, http.StatusAccepted, make(chan int)) // unmarshalable
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // status NOT committed (recorder default)
	assert.Empty(t, rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Type"))
}

func TestJSON_Nil(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSON(rec, http.StatusOK, nil)
	require.NoError(t, err)
	assert.Equal(t, "null\n", rec.Body.String())
}

func TestJSONStream_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSONStream(rec, http.StatusOK, map[string]int{"n": 1})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"n":1}`, rec.Body.String())
}

func TestJSONStream_PassThroughCommitsStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.JSONStream(rec, http.StatusAccepted, make(chan int))
	require.Error(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code) // status WAS committed before the failure
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./render/`
Expected: FAIL — build error, `undefined: render.JSON` / `render.JSONStream` (package has no source yet).

- [ ] **Step 3: Write the implementation** — create `render/content.go`:

```go
package render

import (
	"bytes"
	"net/http"
	"sync"
)

const contentTypeJSON = "application/json; charset=utf-8"

// setContentType sets the Content-Type header to ct only when the caller has not
// already set one, so a handler can pre-set a custom charset/parameters and win.
func setContentType(w http.ResponseWriter, ct string) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", ct)
	}
}

// bufPool backs the transactional encoders (JSON, HTML, Templ).
var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func getBuf() *bytes.Buffer {
	if b, ok := bufPool.Get().(*bytes.Buffer); ok {
		return b
	}
	return new(bytes.Buffer)
}

func putBuf(b *bytes.Buffer) {
	const maxReuse = 1 << 20 // 1 MiB; don't pin large buffers in the pool
	if b.Cap() > maxReuse {
		return
	}
	b.Reset()
	bufPool.Put(b)
}
```

Create `render/json.go`:

```go
package render

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// JSON encodes v as JSON and writes it with the given status code. It is
// transactional: v is encoded into a pooled buffer first, so on an encode error
// nothing is written to w and the error is returned — the caller can still send a
// clean error response. The Content-Type defaults to "application/json; charset=utf-8"
// unless the caller has already set one.
func JSON(w http.ResponseWriter, status int, v any) error {
	buf := getBuf()
	defer putBuf(buf)
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		return fmt.Errorf("render: encode json: %w", err)
	}
	setContentType(w, contentTypeJSON)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write json: %w", err)
	}
	return nil
}

// JSONStream is the streaming counterpart to JSON: it writes the status, then encodes
// v straight to w with no intermediate buffer. Use it for very large payloads where
// buffering the whole document is wasteful. Unlike JSON it is NOT transactional — an
// encode error mid-stream leaves a partial body under the already-sent status, so the
// returned error is only useful for logging. The Content-Type defaults to
// "application/json; charset=utf-8" unless already set.
func JSONStream(w http.ResponseWriter, status int, v any) error {
	setContentType(w, contentTypeJSON)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("render: stream json: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./render/`
Expected: PASS (all `TestJSON*` / `TestJSONStream*`).

- [ ] **Step 5: Commit**

```bash
git add render/content.go render/json.go render/json_test.go
git commit -m "feat(render): JSON and JSONStream response helpers"
```

---

### Task 2: HTML

**Files:**
- Create: `render/html.go`
- Create: `render/errors.go` (`ErrNilTemplate`, `ErrNilComponent` — both exported, so defining the unused-here `ErrNilComponent` now is fine)
- Modify: `render/content.go` (add `const contentTypeHTML`)
- Test: `render/html_test.go`

**Interfaces:**
- Consumes: `getBuf`, `putBuf`, `setContentType` (Task 1).
- Produces:
  - `func HTML(w http.ResponseWriter, status int, t *template.Template, name string, data any) error`
  - `var ErrNilTemplate`, `var ErrNilComponent`
  - internal `const contentTypeHTML`

- [ ] **Step 1: Write the failing test** — create `render/html_test.go`:

```go
package render_test

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestHTML_Execute(t *testing.T) {
	tmpl := template.Must(template.New("x").Parse("Hi {{.}}"))
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusOK, tmpl, "", "Bob")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "Hi Bob", rec.Body.String())
}

func TestHTML_ExecuteTemplateNamed(t *testing.T) {
	tmpl := template.Must(template.New("root").Parse(`{{define "page"}}P:{{.}}{{end}}`))
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusOK, tmpl, "page", "x")
	require.NoError(t, err)
	assert.Equal(t, "P:x", rec.Body.String())
}

func TestHTML_NilTemplate(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusOK, nil, "", nil)
	require.ErrorIs(t, err, render.ErrNilTemplate)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestHTML_ExecuteErrorWritesNothing(t *testing.T) {
	tmpl := template.Must(template.New("x").Funcs(template.FuncMap{
		"boom": func() (string, error) { return "", errors.New("boom") },
	}).Parse("{{boom}}"))
	rec := httptest.NewRecorder()
	err := render.HTML(rec, http.StatusAccepted, tmpl, "", nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code) // not committed
	assert.Empty(t, rec.Body.String())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./render/`
Expected: FAIL — `undefined: render.HTML`, `undefined: render.ErrNilTemplate`.

- [ ] **Step 3: Write the implementation** — create `render/errors.go`:

```go
package render

import "errors"

// ErrNilTemplate is returned by HTML when the provided template is nil.
var ErrNilTemplate = errors.New("render: nil template")

// ErrNilComponent is returned by Templ when the provided component is nil.
var ErrNilComponent = errors.New("render: nil component")
```

Modify `render/content.go` — add the constant directly under `contentTypeJSON`:

```go
const contentTypeJSON = "application/json; charset=utf-8"
const contentTypeHTML = "text/html; charset=utf-8"
```

Create `render/html.go`:

```go
package render

import (
	"fmt"
	"html/template"
	"net/http"
)

// HTML executes an html/template into a pooled buffer, then writes the result with
// the given status code. When name is "" it runs t.Execute(data); otherwise it runs
// t.ExecuteTemplate(name, data) — the layout / {{define}} pattern. It is
// transactional: a template execution error returns with nothing written to w. It
// returns ErrNilTemplate if t is nil (before writing anything). The Content-Type
// defaults to "text/html; charset=utf-8" unless the caller has already set one.
func HTML(w http.ResponseWriter, status int, t *template.Template, name string, data any) error {
	if t == nil {
		return ErrNilTemplate
	}
	buf := getBuf()
	defer putBuf(buf)
	var err error
	if name == "" {
		err = t.Execute(buf, data)
	} else {
		err = t.ExecuteTemplate(buf, name, data)
	}
	if err != nil {
		return fmt.Errorf("render: execute template: %w", err)
	}
	setContentType(w, contentTypeHTML)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write html: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add render/errors.go render/content.go render/html.go render/html_test.go
git commit -m "feat(render): HTML template helper + nil sentinels"
```

---

### Task 3: Templ + Component interface

**Files:**
- Create: `render/templ.go`
- Test: `render/templ_test.go`

**Interfaces:**
- Consumes: `getBuf`, `putBuf`, `setContentType`, `contentTypeHTML`, `ErrNilComponent`.
- Produces:
  - `type Component interface { Render(ctx context.Context, w io.Writer) error }`
  - `func Templ(ctx context.Context, w http.ResponseWriter, status int, c Component) error`

- [ ] **Step 1: Write the failing test** — create `render/templ_test.go`:

```go
package render_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

type testCtxKey struct{}

// fakeComponent satisfies render.Component (and mirrors templ.Component's method).
type fakeComponent struct {
	out      string
	err      error
	ctxValue any
}

func (f *fakeComponent) Render(ctx context.Context, w io.Writer) error {
	f.ctxValue = ctx.Value(testCtxKey{})
	if f.err != nil {
		return f.err
	}
	_, err := io.WriteString(w, f.out)
	return err
}

func TestTempl_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &fakeComponent{out: "<p>hi</p>"}
	ctx := context.WithValue(context.Background(), testCtxKey{}, "v")
	err := render.Templ(ctx, rec, http.StatusOK, c)
	require.NoError(t, err)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "<p>hi</p>", rec.Body.String())
	assert.Equal(t, "v", c.ctxValue) // ctx propagated to the component
}

func TestTempl_NilComponent(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Templ(context.Background(), rec, http.StatusOK, nil)
	require.ErrorIs(t, err, render.ErrNilComponent)
	assert.Empty(t, rec.Body.String())
}

func TestTempl_RenderErrorWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &fakeComponent{err: errors.New("boom")}
	err := render.Templ(context.Background(), rec, http.StatusAccepted, c)
	require.Error(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./render/`
Expected: FAIL — `undefined: render.Templ` / `render.Component`.

- [ ] **Step 3: Write the implementation** — create `render/templ.go`:

```go
package render

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Component is anything that can render itself to an io.Writer. It is structurally
// satisfied by github.com/a-h/templ components (templ.Component has the identical
// Render method), so templ output works without this package importing templ.
type Component interface {
	Render(ctx context.Context, w io.Writer) error
}

// Templ renders c into a pooled buffer, then writes the result with the given status
// code. It is transactional: a Render error returns with nothing written to w. It
// returns ErrNilComponent if c is nil (before writing anything). ctx is the
// per-request context (usually r.Context()). The Content-Type defaults to
// "text/html; charset=utf-8" unless the caller has already set one.
func Templ(ctx context.Context, w http.ResponseWriter, status int, c Component) error {
	if c == nil {
		return ErrNilComponent
	}
	buf := getBuf()
	defer putBuf(buf)
	if err := c.Render(ctx, buf); err != nil {
		return fmt.Errorf("render: render component: %w", err)
	}
	setContentType(w, contentTypeHTML)
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("render: write templ: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add render/templ.go render/templ_test.go
git commit -m "feat(render): Templ helper via dependency-free Component interface"
```

---

### Task 4: Primitives — Text, Blob, NoContent, Redirect

**Files:**
- Create: `render/text.go` (`Text`, `Blob`, `NoContent`)
- Create: `render/redirect.go` (`Redirect`)
- Modify: `render/content.go` (add `const contentTypeText`)
- Test: `render/text_test.go`, `render/redirect_test.go`

**Interfaces:**
- Consumes: `setContentType`, `contentTypeText`.
- Produces:
  - `func Text(w http.ResponseWriter, status int, s string) error`
  - `func Blob(w http.ResponseWriter, status int, contentType string, b []byte) error`
  - `func NoContent(w http.ResponseWriter)`
  - `func Redirect(w http.ResponseWriter, r *http.Request, status int, url string)`

- [ ] **Step 1: Write the failing tests** — create `render/text_test.go`:

```go
package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestText(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Text(rec, http.StatusOK, "pong")
	require.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "pong", rec.Body.String())
}

func TestBlob_WithContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Blob(rec, http.StatusOK, "image/png", []byte{1, 2, 3})
	require.NoError(t, err)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, []byte{1, 2, 3}, rec.Body.Bytes())
}

func TestBlob_EmptyContentTypeNotSet(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Blob(rec, http.StatusOK, "", []byte("data"))
	require.NoError(t, err)
	assert.Empty(t, rec.Header().Get("Content-Type")) // we don't set it; left to sniffing
}

func TestNoContent(t *testing.T) {
	rec := httptest.NewRecorder()
	render.NoContent(rec)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}
```

Create `render/redirect_test.go`:

```go
package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/render"
)

func TestRedirect(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	render.Redirect(rec, req, http.StatusSeeOther, "/new")
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/new", rec.Header().Get("Location"))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `just test ./render/`
Expected: FAIL — `undefined: render.Text` / `Blob` / `NoContent` / `Redirect`.

- [ ] **Step 3: Write the implementation** — modify `render/content.go` (add the constant under the others):

```go
const contentTypeText = "text/plain; charset=utf-8"
```

Create `render/text.go`:

```go
package render

import (
	"fmt"
	"io"
	"net/http"
)

// Text writes s with the given status code as "text/plain; charset=utf-8" (unless the
// caller has already set a Content-Type).
func Text(w http.ResponseWriter, status int, s string) error {
	setContentType(w, contentTypeText)
	w.WriteHeader(status)
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("render: write text: %w", err)
	}
	return nil
}

// Blob writes b with the given status code. If contentType is non-empty it is used
// (unless the caller has already set a Content-Type); otherwise net/http sniffs the
// body on first write.
func Blob(w http.ResponseWriter, status int, contentType string, b []byte) error {
	if contentType != "" {
		setContentType(w, contentType)
	}
	w.WriteHeader(status)
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("render: write blob: %w", err)
	}
	return nil
}

// NoContent writes 204 No Content with no body. It cannot fail, so it returns nothing.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}
```

Create `render/redirect.go`:

```go
package render

import "net/http"

// Redirect issues an HTTP redirect to url with the given 3xx status code (e.g.
// http.StatusFound or http.StatusSeeOther). It is a thin wrapper over http.Redirect.
func Redirect(w http.ResponseWriter, r *http.Request, status int, url string) {
	http.Redirect(w, r, url, status)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add render/content.go render/text.go render/redirect.go render/text_test.go render/redirect_test.go
git commit -m "feat(render): Text, Blob, NoContent, Redirect primitives"
```

---

### Task 5: CSV (+ Content-Disposition encoder)

**Files:**
- Create: `render/csv.go`
- Modify: `render/content.go` (add `const contentTypeCSV`; add `contentDisposition`, `baseName`, `rfc5987Encode`, `isAttrChar`; add `fmt`, `strings` imports)
- Test: `render/csv_test.go`

**Interfaces:**
- Consumes: `setContentType`, `contentTypeCSV`.
- Produces:
  - `func CSV(w http.ResponseWriter, status int, filename string, records [][]string) error`
  - internal `func contentDisposition(disposition, filename string) string` (consumed by Task 6)

- [ ] **Step 1: Write the failing test** — create `render/csv_test.go`:

```go
package render_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestCSV_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.CSV(rec, http.StatusOK, "", [][]string{{"a", "b"}, {"1", "2"}})
	require.NoError(t, err)
	assert.Equal(t, "text/csv; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Equal(t, "a,b\n1,2\n", rec.Body.String())
	assert.Empty(t, rec.Header().Get("Content-Disposition"))
}

func TestCSV_WithFilenameSetsDisposition(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.CSV(rec, http.StatusOK, "users.csv", [][]string{{"x"}})
	require.NoError(t, err)
	assert.Equal(t,
		`attachment; filename="users.csv"; filename*=UTF-8''users.csv`,
		rec.Header().Get("Content-Disposition"))
}

func TestCSV_EmptyRecords(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.CSV(rec, http.StatusOK, "", nil)
	require.NoError(t, err)
	assert.Empty(t, rec.Body.String())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./render/`
Expected: FAIL — `undefined: render.CSV`.

- [ ] **Step 3: Write the implementation** — modify `render/content.go`: add the constant, change the import block to include `fmt` and `strings`, and append the disposition helpers. The import block becomes:

```go
import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
)
```

Add the constant with the others:

```go
const contentTypeCSV = "text/csv; charset=utf-8"
```

Append the helpers at the end of `render/content.go`:

```go
// contentDisposition builds a Content-Disposition header value for a download, e.g.
//   attachment; filename="report.csv"; filename*=UTF-8''r%C3%A9sum%C3%A9.csv
// It uses only the base name of filename, builds an injection-safe quoted ASCII
// fallback (control/quote/backslash/non-ASCII bytes replaced with '_'), and appends
// an RFC 5987 filename* with the exact UTF-8 name percent-encoded. When the name
// reduces to empty, only the disposition type is returned.
func contentDisposition(disposition, filename string) string {
	name := baseName(filename)
	if name == "" {
		return disposition
	}
	var ascii strings.Builder
	ascii.Grow(len(name))
	for i := range len(name) {
		c := name[i]
		if c < 0x20 || c == 0x7f || c == '"' || c == '\\' || c >= 0x80 {
			ascii.WriteByte('_')
		} else {
			ascii.WriteByte(c)
		}
	}
	return fmt.Sprintf("%s; filename=%q; filename*=UTF-8''%s",
		disposition, ascii.String(), rfc5987Encode(name))
}

// baseName returns the final path element, splitting on both '/' and '\' so a
// Windows-style or POSIX path cannot inject directory components.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// rfc5987Encode percent-encodes s per RFC 5987 ext-value (UTF-8), leaving only the
// attr-char set unescaped.
func rfc5987Encode(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperhex[c>>4])
		b.WriteByte(upperhex[c&0x0f])
	}
	return b.String()
}

// isAttrChar reports whether c is in the RFC 5987 attr-char set.
func isAttrChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}
```

Create `render/csv.go`:

```go
package render

import (
	"encoding/csv"
	"fmt"
	"net/http"
)

// CSV streams records as "text/csv; charset=utf-8" with the given status code. When
// filename is non-empty a Content-Disposition: attachment header is set with an RFC
// 5987-safe filename. CSV streams (it is not buffered), so a write error mid-output
// may leave a partial body; the returned error is for logging.
func CSV(w http.ResponseWriter, status int, filename string, records [][]string) error {
	setContentType(w, contentTypeCSV)
	if filename != "" {
		w.Header().Set("Content-Disposition", contentDisposition("attachment", filename))
	}
	w.WriteHeader(status)
	cw := csv.NewWriter(w)
	if err := cw.WriteAll(records); err != nil { // WriteAll flushes internally
		return fmt.Errorf("render: write csv: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add render/content.go render/csv.go render/csv_test.go
git commit -m "feat(render): CSV helper with RFC 5987-safe Content-Disposition"
```

---

### Task 6: Stream & Attachment

**Files:**
- Create: `render/stream.go`
- Modify: `render/content.go` (add `const contentTypeOctet`)
- Test: `render/stream_test.go`

**Interfaces:**
- Consumes: `setContentType`, `contentTypeOctet`, `contentDisposition` (Task 5).
- Produces:
  - `func Stream(w http.ResponseWriter, status int, contentType string, body io.Reader) error`
  - `func Attachment(w http.ResponseWriter, status int, filename, contentType string, body io.Reader) error`

- [ ] **Step 1: Write the failing test** — create `render/stream_test.go`:

```go
package render_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestStream_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Stream(rec, http.StatusOK, "text/plain", strings.NewReader("hello"))
	require.NoError(t, err)
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
	assert.Equal(t, "hello", rec.Body.String())
}

func TestAttachment_DefaultsOctetAndSetsDisposition(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Attachment(rec, http.StatusOK, "f.bin", "", strings.NewReader("x"))
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t,
		`attachment; filename="f.bin"; filename*=UTF-8''f.bin`,
		rec.Header().Get("Content-Disposition"))
	assert.Equal(t, "x", rec.Body.String())
}

func TestStream_ReaderErrorPropagates(t *testing.T) {
	rec := httptest.NewRecorder()
	err := render.Stream(rec, http.StatusOK, "text/plain", errReader{})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./render/`
Expected: FAIL — `undefined: render.Stream` / `render.Attachment`.

- [ ] **Step 3: Write the implementation** — modify `render/content.go` (add the constant with the others):

```go
const contentTypeOctet = "application/octet-stream"
```

Create `render/stream.go`:

```go
package render

import (
	"fmt"
	"io"
	"net/http"
)

// Stream copies body to the response inline with the given status code. If contentType
// is non-empty it is used (unless the caller has already set one); otherwise net/http
// sniffs. It is pass-through: a copy error mid-stream may leave a partial body, so the
// returned error is for logging. Use it to proxy an io.Reader (e.g. an upstream or S3
// response body) inline.
func Stream(w http.ResponseWriter, status int, contentType string, body io.Reader) error {
	if contentType != "" {
		setContentType(w, contentType)
	}
	w.WriteHeader(status)
	if _, err := io.Copy(w, body); err != nil {
		return fmt.Errorf("render: stream: %w", err)
	}
	return nil
}

// Attachment is Stream plus a Content-Disposition: attachment header with an RFC
// 5987-safe filename. contentType defaults to "application/octet-stream" when empty
// (download intent). Use it for generated downloads (a built CSV/PDF, an export
// stream, or a proxied object you want saved rather than displayed).
func Attachment(w http.ResponseWriter, status int, filename, contentType string, body io.Reader) error {
	if contentType == "" {
		contentType = contentTypeOctet
	}
	setContentType(w, contentType)
	if filename != "" {
		w.Header().Set("Content-Disposition", contentDisposition("attachment", filename))
	}
	w.WriteHeader(status)
	if _, err := io.Copy(w, body); err != nil {
		return fmt.Errorf("render: attachment: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add render/content.go render/stream.go render/stream_test.go
git commit -m "feat(render): Stream and Attachment io.Reader helpers"
```

---

### Task 7: File & FileFS

**Files:**
- Create: `render/file.go`
- Test: `render/file_test.go`

**Interfaces:**
- Consumes: nothing internal (delegates to stdlib).
- Produces:
  - `func File(w http.ResponseWriter, r *http.Request, path string)`
  - `func FileFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string)`

- [ ] **Step 1: Write the failing test** — create `render/file_test.go`:

```go
package render_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestFile_ServesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o600))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/hello.txt", nil)
	render.File(rec, req, path)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "hello world", rec.Body.String())
}

func TestFileFS_ServesFromFS(t *testing.T) {
	fsys := fstest.MapFS{"logo.svg": &fstest.MapFile{Data: []byte("<svg/>")}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/logo.svg", nil)
	render.FileFS(rec, req, fsys, "logo.svg")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<svg/>", rec.Body.String())
}

func TestFile_RangeRequestReturns206(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	require.NoError(t, os.WriteFile(path, []byte("0123456789"), 0o600))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data.txt", nil)
	req.Header.Set("Range", "bytes=0-3")
	render.File(rec, req, path)
	assert.Equal(t, http.StatusPartialContent, rec.Code) // proves stdlib delegation
	assert.Equal(t, "0123", rec.Body.String())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `just test ./render/`
Expected: FAIL — `undefined: render.File` / `render.FileFS`.

- [ ] **Step 3: Write the implementation** — create `render/file.go`:

```go
package render

import (
	"io/fs"
	"net/http"
)

// File serves a single local file by path via http.ServeFile, which handles Range
// requests, If-Modified-Since, and content-type sniffing. path is server-trusted — do
// NOT pass an unsanitized user-supplied path; use FileFS with a rooted fs.FS for
// user-influenced names. Status and error handling are owned by http.ServeFile.
func File(w http.ResponseWriter, r *http.Request, path string) {
	http.ServeFile(w, r, path)
}

// FileFS serves name from fsys via http.ServeFileFS — the safe, rooted form. Pass
// os.DirFS("/var/www") for a directory root, or an embed.FS for bundled assets; name
// resolution is constrained to fsys.
func FileFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	http.ServeFileFS(w, r, fsys, name)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add render/file.go render/file_test.go
git commit -m "feat(render): File and FileFS local-file helpers"
```

---

### Task 8: Cross-cutting tests (filename safety, set-if-empty, write errors)

**Files:**
- Test: `render/header_test.go` (no production code — exercises the public surface only)

**Interfaces:**
- Consumes: `render.Attachment`, `render.JSON`, `render.Text`, `render.HTML`, `render.Blob`, `render.Stream` (all prior tasks).
- Produces: nothing.

- [ ] **Step 1: Write the failing test** — create `render/header_test.go`:

```go
package render_test

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/render"
)

func TestContentDisposition_FilenameSafety(t *testing.T) {
	tests := []struct {
		name, filename, want string
	}{
		{
			name:     "ascii",
			filename: "report.csv",
			want:     `attachment; filename="report.csv"; filename*=UTF-8''report.csv`,
		},
		{
			name:     "unicode each byte underscored in fallback",
			filename: "résumé.csv",
			want:     `attachment; filename="r__sum__.csv"; filename*=UTF-8''r%C3%A9sum%C3%A9.csv`,
		},
		{
			name:     "strips path components",
			filename: "../../etc/passwd",
			want:     `attachment; filename="passwd"; filename*=UTF-8''passwd`,
		},
		{
			name:     "quote and crlf injection neutralized",
			filename: "a\"b\r\nc.csv",
			want:     `attachment; filename="a_b__c.csv"; filename*=UTF-8''a%22b%0D%0Ac.csv`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := render.Attachment(rec, http.StatusOK, tt.filename,
				"application/octet-stream", strings.NewReader("x"))
			require.NoError(t, err)
			assert.Equal(t, tt.want, rec.Header().Get("Content-Disposition"))
		})
	}
}

func TestContentType_PresetWinsAcrossWriters(t *testing.T) {
	const custom = "text/plain; charset=iso-8859-1"

	t.Run("JSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", custom)
		require.NoError(t, render.JSON(rec, http.StatusOK, 1))
		assert.Equal(t, custom, rec.Header().Get("Content-Type"))
	})
	t.Run("Text", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", custom)
		require.NoError(t, render.Text(rec, http.StatusOK, "x"))
		assert.Equal(t, custom, rec.Header().Get("Content-Type"))
	})
	t.Run("HTML", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", custom)
		tmpl := template.Must(template.New("x").Parse("hi"))
		require.NoError(t, render.HTML(rec, http.StatusOK, tmpl, "", nil))
		assert.Equal(t, custom, rec.Header().Get("Content-Type"))
	})
}

// failWriter is an http.ResponseWriter whose Write always fails.
type failWriter struct {
	header http.Header
	code   int
}

func newFailWriter() *failWriter             { return &failWriter{header: make(http.Header)} }
func (f *failWriter) Header() http.Header     { return f.header }
func (f *failWriter) WriteHeader(code int)    { f.code = code }
func (f *failWriter) Write([]byte) (int, error) {
	return 0, errors.New("connection reset")
}

func TestWriteErrorPropagates(t *testing.T) {
	t.Run("JSON", func(t *testing.T) {
		require.Error(t, render.JSON(newFailWriter(), http.StatusOK, map[string]int{"n": 1}))
	})
	t.Run("Text", func(t *testing.T) {
		require.Error(t, render.Text(newFailWriter(), http.StatusOK, "x"))
	})
	t.Run("Blob", func(t *testing.T) {
		require.Error(t, render.Blob(newFailWriter(), http.StatusOK, "text/plain", []byte("x")))
	})
	t.Run("Stream", func(t *testing.T) {
		require.Error(t, render.Stream(newFailWriter(), http.StatusOK, "text/plain", strings.NewReader("x")))
	})
}
```

- [ ] **Step 2: Run the test to verify it fails (or surfaces a bug)**

Run: `just test ./render/`
Expected: PASS immediately if Tasks 1–6 are correct — this task is a verification net for cross-cutting behavior. If any case FAILS, fix the underlying production code in the relevant file (e.g. the `contentDisposition` encoder) before committing. (For a strict red-first cycle, you may temporarily assert a wrong `want`, watch it fail, then correct it.)

- [ ] **Step 3: (Only if a case failed) fix production code**

If `TestContentDisposition_FilenameSafety` fails, re-check `contentDisposition`/`rfc5987Encode`/`isAttrChar` in `render/content.go` against the expected values above. No new production code is expected otherwise.

- [ ] **Step 4: Run the test to verify it passes**

Run: `just test ./render/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add render/header_test.go
git commit -m "test(render): cross-cutting header, filename-safety, write-error coverage"
```

---

### Task 9: Package doc, runnable Example, and full check

**Files:**
- Create: `render/doc.go`
- Create: `render/example_test.go`

**Interfaces:**
- Consumes: `render.JSON` (in the Example).
- Produces: package documentation; one runnable `Example`.

- [ ] **Step 1: Write the failing Example test** — create `render/example_test.go`:

```go
package render_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/render"
)

func ExampleJSON() {
	rec := httptest.NewRecorder()
	_ = render.JSON(rec, http.StatusOK, map[string]string{"status": "ok"})

	fmt.Println(rec.Code)
	fmt.Print(rec.Body.String())
	// Output:
	// 200
	// {"status":"ok"}
}
```

- [ ] **Step 2: Run the Example to verify it fails**

Run: `just test ./render/`
Expected: PASS for the Example *if* the body matches. (If it fails on output mismatch, the encoder appends a trailing newline — `fmt.Print` of the body already includes it; the `// Output:` block above accounts for that. Adjust only if Go reports a diff.)

- [ ] **Step 3: Write the package doc** — create `render/doc.go`:

```go
// Package render provides small, stateless helpers for writing HTTP responses from a
// handler: JSON/JSONStream, HTML (html/template), Templ (a-h/templ components, via a
// structural interface — no dependency), Text, Blob, CSV, Stream, Attachment,
// File/FileFS, Redirect, and NoContent.
//
// The helpers are free functions — there is no constructor, options, or global state.
// The caller owns its *template.Template and handles the returned error:
//
//	func handle(w http.ResponseWriter, r *http.Request) {
//		if err := render.JSON(w, http.StatusOK, user); err != nil {
//			// log err; the response may be incomplete
//		}
//	}
//
// JSON, HTML, and Templ are transactional: they encode into a pooled buffer first, so
// an encode/template error returns with nothing written and the caller can still send
// a clean error status. The streaming writers (JSONStream, CSV, Stream, Attachment)
// write directly, so a mid-stream error may leave a partial body and the returned
// error is only useful for logging. Content-Type is set only when the caller has not
// already set one.
//
// render does not negotiate content (the handler picks the function) and never fetches
// remote URLs: serve an S3 object with Redirect, or fetch it in the handler and pass
// the body to Stream/Attachment.
package render
```

- [ ] **Step 4: Run the full check**

Run: `just check`
Expected: `fmt` makes no unexpected changes; `lint` passes (`go vet`, `go build`, `golangci-lint`, `nilaway`, `betteralign`, `modernize` all clean); `test` passes with `-race -cover`. Fix any lint finding before committing — common ones: `goimports` grouping (run `just fmt`), a `modernize` rewrite suggestion, or `betteralign` field-order note on a test struct.

- [ ] **Step 5: Commit**

```bash
git add render/doc.go render/example_test.go
git commit -m "docs(render): package doc and runnable Example"
```

---

## Self-Review

**1. Spec coverage** (each spec section → task):
- JSON (transactional) + JSONStream (streaming) → Task 1 ✓
- HTML + nil-guard sentinels → Task 2 ✓
- Templ + dependency-free `Component` → Task 3 ✓
- Text / Blob / NoContent / Redirect (own file) → Task 4 ✓
- CSV + RFC 5987 `Content-Disposition` encoder → Task 5 ✓
- Stream / Attachment (io.Reader, octet default) → Task 6 ✓
- File / FileFS (Range/sniffing via stdlib) → Task 7 ✓
- Cross-cutting: set-if-empty, filename safety table, write-error propagation → Task 8 ✓
- Black-box tests in `package render_test` → every test file ✓
- `doc.go` + runnable Example → Task 9 ✓
- Non-goals (no content negotiation, no remote fetch, no config/options) → enforced by absence; remote/S3 covered by Redirect + Stream/Attachment (documented in `doc.go`) ✓
- Buffer-pool `maxReuse` (no direct test — internal) → present in Task 1; benchmark deliberately omitted as optional per spec ✓

**2. Placeholder scan:** No TBD/TODO; every code step contains complete, compilable code and exact commands. ✓

**3. Type consistency:** Signatures match across tasks — `setContentType(w, ct)`, `getBuf()/putBuf(b)`, `contentDisposition(disposition, filename)`, `Component.Render(ctx, w)`, and the content-type constants (`contentTypeJSON/HTML/Text/CSV/Octet`) are defined once and consumed by the named tasks. `Stream`/`Attachment`/`CSV` filename handling all route through the single `contentDisposition` helper. ✓

**Note on test ordering:** Task 8 (cross-cutting tests) and Task 9's Example are largely verification nets over code written in Tasks 1–7; they should pass on first run. They are kept as separate tasks so a reviewer can gate the public-surface guarantees and documentation independently.
