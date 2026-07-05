# Wave 1 Unblocker Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add eight small, independent API additions across seven shipped forge packages (plus a hostrouter doc note and a packages.md table edit) that unblock the core-tier roadmap.

**Architecture:** Each task extends one already-shipped package with a focused addition following that package's existing conventions. Tasks are fully independent (distinct packages, no shared symbols), so they can be implemented in any order or in parallel. Every addition is crypto-secure / stdlib-only where relevant, uses the options pattern (never builders), and ships `errors.Is`-matchable sentinels.

**Tech Stack:** Go 1.26, stdlib (`crypto/rand`, `log/slog`, `net/http`, `mime/multipart`, `os/signal`), `github.com/stretchr/testify` (assert/require). Reference spec: `docs/superpowers/specs/2026-07-05-wave1-unblockers-design.md`.

## Global Constraints

- **Work only in the current branch** (`claude/optimistic-sutherland-ac07eb`) — never switch branches.
- **Black-box tests only**: test files use `package <pkg>_test` and import the package by its full path. White-box (`package <pkg>`) is allowed *only* to assert unexported state — none is needed here.
- **Options, never builders**: configuration is `func(*config)` variadics.
- **`errors.Is`-matchable single-line sentinels**: `var Err… = errors.New("pkg: …")`.
- **Public methods never return unexported types.**
- **Go 1.26 idioms**: `for i := range n` integer ranges; `new(expr)` (never a `ptr.To` helper); `errors.AsType`. `just lint` runs `modernize` and will flag violations.
- **Assertions**: `github.com/stretchr/testify/assert` and `.../require` (require for setup that must not continue on failure). No dot-imports.
- **Imports**: goimports local-prefix is `github.com/dmitrymomot/forge` (handled by `just fmt`).
- **Per-package formatting**: run `just fmt ./<domain>/<pkg>/...` (package-path form). The single-file form (`just fmt path/file.go`) trips a spurious betteralign "undefined" — always use the `./…/...` form.
- **Per-task close-out**: `just fmt ./<domain>/<pkg>/...` → `just lint` → `just test ./<domain>/<pkg>/...` must all pass before committing.
- **No Claude attribution** in commit messages (no "Generated with", no `Co-Authored-By: Claude`).

---

## Task 1: `core/random` — bias-free `String` + `DigitCode`

**Files:**
- Modify: `core/random/random.go` (add constants + `String` + `DigitCode` + `dedupeCharset`)
- Test: `core/random/random_test.go` (`package random_test`, already exists — append)

**Interfaces:**
- Consumes: existing `random.Bytes`, `crypto/rand` (imported as `rand`), `fmt`.
- Produces:
  - `const Lowercase, Uppercase, Digits, Alphabetic, Alphanumeric, Symbols string`
  - `func String(n int, charsets ...string) string`
  - `func DigitCode(n int) string`

- [ ] **Step 1: Write the failing tests**

Append to `core/random/random_test.go` (ensure `strings` is imported in the test file):

```go
func TestString_LengthAndAlphabet(t *testing.T) {
	for range 100 {
		s := random.String(16, random.Uppercase, random.Digits)
		assert.Len(t, s, 16)
		for _, c := range s {
			assert.True(t, strings.ContainsRune(random.Uppercase+random.Digits, c), "unexpected char %q", c)
		}
	}
}

func TestString_DefaultIsAlphanumeric(t *testing.T) {
	s := random.String(32)
	assert.Len(t, s, 32)
	for _, c := range s {
		assert.True(t, strings.ContainsRune(random.Alphanumeric, c))
	}
}

func TestString_Zero(t *testing.T) {
	assert.Equal(t, "", random.String(0))
}

func TestString_DedupesOverlap(t *testing.T) {
	// Alphanumeric already contains Digits; passing both must not bias digits.
	const n = 20000
	s := random.String(n, random.Alphanumeric, random.Digits)
	digits := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	frac := float64(digits) / float64(n)
	// 10 of 62 distinct chars are digits => ~0.161. Un-deduped (2x) would be ~0.278.
	assert.InDelta(t, 10.0/62.0, frac, 0.03, "digit fraction suggests charset not de-duplicated")
}

func TestString_PanicsOnEmptyCharsetOrNegative(t *testing.T) {
	assert.Panics(t, func() { random.String(-1) })
	assert.Panics(t, func() { random.String(4, "") })
}

func TestDigitCode(t *testing.T) {
	for range 100 {
		c := random.DigitCode(6)
		assert.Len(t, c, 6)
		for _, r := range c {
			assert.True(t, r >= '0' && r <= '9', "non-digit %q", r)
		}
	}
}

func TestDigitCode_LeadingZerosPossible(t *testing.T) {
	seen := false
	for range 3000 {
		if random.DigitCode(4)[0] == '0' {
			seen = true
			break
		}
	}
	assert.True(t, seen, "leading zero should be possible")
}

func TestDigitCode_PanicsOnNonPositive(t *testing.T) {
	assert.Panics(t, func() { random.DigitCode(0) })
	assert.Panics(t, func() { random.DigitCode(-1) })
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/random -run 'TestString|TestDigitCode' -v`
Expected: FAIL — `undefined: random.String` / `random.Digits` etc.

- [ ] **Step 3: Write the implementation**

Append to `core/random/random.go` (add `"strings"` to the import block; `crypto/rand` is already imported as `rand`, `fmt` is already imported):

```go
// Charset constants for String. Each is an ASCII byte string.
const (
	Lowercase    = "abcdefghijklmnopqrstuvwxyz"
	Uppercase    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	Digits       = "0123456789"
	Alphabetic   = Lowercase + Uppercase
	Alphanumeric = Alphabetic + Digits
	Symbols      = "~!@#$%^&*()-_+={}[]|\\;:\"<>,./?`"
)

// String returns a cryptographically secure random string of n characters drawn
// uniformly (bias-free, via crypto/rand rejection sampling) from the concatenation
// of charsets. With no charsets it defaults to Alphanumeric. Overlapping charsets
// are de-duplicated (first occurrence wins) so the distribution stays uniform over
// the distinct characters. The charset is treated as bytes; multi-byte UTF-8
// alphabets are not supported. It panics if n < 0 or the combined charset is empty.
func String(n int, charsets ...string) string {
	if n < 0 {
		panic("random: String n must be >= 0")
	}
	set := dedupeCharset(charsets)
	k := len(set)
	if k == 0 {
		panic("random: String charset is empty")
	}
	if n == 0 {
		return ""
	}
	out := make([]byte, n)
	// max is the largest multiple of k within a byte; bytes at or above it are
	// rejected to remove modulo bias. De-duping over bytes guarantees k <= 256.
	max := 256 - (256 % k)
	buf := make([]byte, n)
	bi := len(buf) // force an initial fill
	for i := 0; i < n; {
		if bi >= len(buf) {
			if _, err := rand.Read(buf); err != nil {
				panic(fmt.Errorf("random: crypto/rand failed: %w", err))
			}
			bi = 0
		}
		b := int(buf[bi])
		bi++
		if b < max {
			out[i] = set[b%k]
			i++
		}
	}
	return string(out)
}

// DigitCode returns an n-digit decimal string with leading zeros preserved,
// suitable for OTP and email verification codes. It panics if n <= 0.
func DigitCode(n int) string {
	if n <= 0 {
		panic("random: DigitCode n must be > 0")
	}
	return String(n, Digits)
}

// dedupeCharset joins charsets (defaulting to Alphanumeric) and removes duplicate
// bytes, preserving first-occurrence order, so String stays uniform over distinct
// characters.
func dedupeCharset(charsets []string) []byte {
	joined := Alphanumeric
	if len(charsets) > 0 {
		joined = strings.Join(charsets, "")
	}
	var seen [256]bool
	out := make([]byte, 0, len(joined))
	for i := range len(joined) {
		c := joined[i]
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./core/random -run 'TestString|TestDigitCode' -v`
Expected: PASS (all sub-tests).

- [ ] **Step 5: Update the package doc**

Add a short paragraph to `core/random/doc.go` documenting `String`/`DigitCode` and the charset constants, and noting `String` is crypto-secure (bias-free) while callers who need the error escape hatch use `Read`.

- [ ] **Step 6: Format, lint, test, commit**

```bash
just fmt ./core/random/...
just lint
just test ./core/random/...
git add core/random/random.go core/random/random_test.go core/random/doc.go
git commit -m "feat(core/random): bias-free String and DigitCode with charset constants"
```

---

## Task 2: `core/id` — bound `Prefix` codec + static aliases

**Files:**
- Create: `core/id/prefix.go` (type, options, methods, static aliases)
- Modify: `core/id/errors.go` (add `ErrWrongPrefix`)
- Test: `core/id/prefix_test.go` (`package id_test`)

**Interfaces:**
- Consumes: `id.NewShort() Short`, `Short.String() string`, `id.ParseShort(string) (Short, error)`, `id.NewULID()`, `id.ParseULID`, existing `id.ErrMalformed`.
- Produces:
  - `type Prefix struct{…}` · `type PrefixOption func(*Prefix)`
  - `func WithGenerator(gen func() string) PrefixOption`
  - `func NewPrefix(prefix string, opts ...PrefixOption) Prefix`
  - `func (Prefix) New() string` · `func (Prefix) Parse(string) (string, error)` · `func (Prefix) Is(string) bool` · `func (Prefix) Prefix() string`
  - `func NewPrefixed(prefix string) string` · `func ParsePrefixed(prefix, s string) (string, error)` · `func IsPrefixed(prefix, s string) bool`
  - `var ErrWrongPrefix error`

- [ ] **Step 1: Write the failing tests**

Create `core/id/prefix_test.go`:

```go
package id_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefix_RoundTripDefault(t *testing.T) {
	p := id.NewPrefix("user")
	s := p.New()
	assert.True(t, strings.HasPrefix(s, "user_"))
	body, err := p.Parse(s)
	require.NoError(t, err)
	_, err = id.ParseShort(body)
	require.NoError(t, err, "default body must decode as Short")
	assert.True(t, p.Is(s))
	assert.Equal(t, "user", p.Prefix())
}

func TestPrefix_CustomGenerator(t *testing.T) {
	p := id.NewPrefix("tok", id.WithGenerator(func() string { return id.NewULID().String() }))
	s := p.New()
	assert.True(t, strings.HasPrefix(s, "tok_"))
	body, err := p.Parse(s)
	require.NoError(t, err)
	_, err = id.ParseULID(body)
	require.NoError(t, err)
}

func TestPrefix_WrongPrefix(t *testing.T) {
	p := id.NewPrefix("user")
	_, err := p.Parse("org_" + id.NewShort().String())
	assert.ErrorIs(t, err, id.ErrWrongPrefix)
	assert.False(t, p.Is("org_abc"))
}

func TestPrefix_EmptyBody(t *testing.T) {
	p := id.NewPrefix("user")
	_, err := p.Parse("user_")
	assert.ErrorIs(t, err, id.ErrMalformed)
}

func TestPrefix_NoSeparator(t *testing.T) {
	p := id.NewPrefix("user")
	_, err := p.Parse("userabc")
	assert.ErrorIs(t, err, id.ErrWrongPrefix)
}

func TestPrefixed_StaticAliases(t *testing.T) {
	s := id.NewPrefixed("acct")
	assert.True(t, strings.HasPrefix(s, "acct_"))
	body, err := id.ParsePrefixed("acct", s)
	require.NoError(t, err)
	assert.NotEmpty(t, body)
	assert.True(t, id.IsPrefixed("acct", s))
	assert.False(t, id.IsPrefixed("other", s))
}

func TestPrefix_PanicsOnInvalid(t *testing.T) {
	assert.Panics(t, func() { id.NewPrefix("") })
	assert.Panics(t, func() { id.NewPrefix("User") })
	assert.Panics(t, func() { id.NewPrefix("a_b") })
	assert.Panics(t, func() { id.NewPrefix("x", id.WithGenerator(nil)) })
	assert.Panics(t, func() { id.NewPrefixed("") })
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./core/id -run TestPrefix -v`
Expected: FAIL — `undefined: id.NewPrefix` etc.

- [ ] **Step 3: Add the sentinel**

Append to `core/id/errors.go` (already `package id`, already imports `errors`):

```go
// ErrWrongPrefix is returned by Prefix.Parse (and ParsePrefixed) when the input
// does not carry the expected "<prefix>_" head. Match it with errors.Is.
var ErrWrongPrefix = errors.New("id: wrong prefix")
```

- [ ] **Step 4: Write the implementation**

Create `core/id/prefix.go`:

```go
package id

import "strings"

// Prefix is an immutable, concurrency-safe Stripe-style ID codec: a human prefix
// joined to a generated body by "_". The zero value is unusable; construct with
// NewPrefix. Options are optional; the default body generator is Short.
type Prefix struct {
	gen    func() string
	prefix string
	joined string // prefix + "_"
}

// PrefixOption configures NewPrefix.
type PrefixOption func(*Prefix)

// WithGenerator sets the body generator — the part after "<prefix>_". The default
// is Short (NewShort().String()); pass any func to mint ULID/UUID/random bodies,
// e.g. WithGenerator(func() string { return NewULID().String() }). A nil gen
// panics via NewPrefix.
func WithGenerator(gen func() string) PrefixOption {
	return func(p *Prefix) { p.gen = gen }
}

// NewPrefix returns a codec emitting IDs of the form "<prefix>_<body>". prefix
// must be non-empty and match [a-z0-9]+ (Stripe convention; "_" is the separator).
// Options are optional. It panics on an invalid prefix or a nil generator — both
// are boot-time programming errors.
func NewPrefix(prefix string, opts ...PrefixOption) Prefix {
	if !validPrefix(prefix) {
		panic("id: NewPrefix prefix must match [a-z0-9]+")
	}
	p := Prefix{
		prefix: prefix,
		joined: prefix + "_",
		gen:    func() string { return NewShort().String() },
	}
	for _, o := range opts {
		o(&p)
	}
	if p.gen == nil {
		panic("id: NewPrefix generator must not be nil")
	}
	return p
}

// New returns a fresh ID: "<prefix>_" + gen().
func (p Prefix) New() string { return p.joined + p.gen() }

// Parse validates that s carries p's prefix and a non-empty body, returning the
// body (the part after "<prefix>_"). Because the body generator is pluggable, the
// body is returned opaque — for the default Short generator, decode it with
// ParseShort. A wrong/absent prefix returns ErrWrongPrefix; an empty body returns
// ErrMalformed.
func (p Prefix) Parse(s string) (string, error) {
	body, ok := strings.CutPrefix(s, p.joined)
	if !ok {
		return "", ErrWrongPrefix
	}
	if body == "" {
		return "", ErrMalformed
	}
	return body, nil
}

// Is reports whether s carries p's prefix and a non-empty body. It validates the
// prefix and body presence only, not the body's internal format.
func (p Prefix) Is(s string) bool {
	_, err := p.Parse(s)
	return err == nil
}

// Prefix returns the bound prefix (for logging / diagnostics).
func (p Prefix) Prefix() string { return p.prefix }

// NewPrefixed returns a fresh "<prefix>_<short>" ID using the default Short
// generator. Equivalent to NewPrefix(prefix).New(); it panics on an invalid prefix.
func NewPrefixed(prefix string) string { return NewPrefix(prefix).New() }

// ParsePrefixed validates prefix on s and returns the body. Equivalent to
// NewPrefix(prefix).Parse(s).
func ParsePrefixed(prefix, s string) (string, error) { return NewPrefix(prefix).Parse(s) }

// IsPrefixed reports whether s carries prefix with a non-empty body. Equivalent to
// NewPrefix(prefix).Is(s).
func IsPrefixed(prefix, s string) bool { return NewPrefix(prefix).Is(s) }

// validPrefix reports whether s is a non-empty [a-z0-9]+ string.
func validPrefix(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./core/id -run TestPrefix -v`
Expected: PASS.

- [ ] **Step 6: Format, lint, test, commit**

```bash
just fmt ./core/id/...
just lint
just test ./core/id/...
git add core/id/prefix.go core/id/errors.go core/id/prefix_test.go
git commit -m "feat(core/id): Prefix codec with pluggable generator and static aliases"
```

> Note: `just fmt` (betteralign) may reorder the `Prefix` struct fields for alignment — that is expected; commit the formatted result.

---

## Task 3: `web/problem` — `Decode` + `*Problem` as an error

**Files:**
- Modify: `web/problem/problem.go` (add `ErrNotProblem`, `Decode`, `(*Problem).Error`, `(*Problem).Is`)
- Test: `web/problem/problem_test.go` (`package problem_test` — append)

**Interfaces:**
- Consumes: existing `problem.Problem` struct.
- Produces:
  - `var ErrNotProblem error`
  - `func (*Problem) Error() string` · `func (*Problem) Is(target error) bool`
  - `func Decode(resp *http.Response) (*Problem, error)`

- [ ] **Step 1: Write the failing tests**

Append to `web/problem/problem_test.go` (ensure `errors`, `io`, `net/http`, `strings` are imported in the test file):

```go
func TestProblem_ErrorString(t *testing.T) {
	p := &problem.Problem{Status: 429, Title: "Too Many Requests", Code: "rate_limited"}
	assert.Equal(t, "problem: 429 Too Many Requests [rate_limited]", p.Error())
	p2 := &problem.Problem{Status: 400, Title: "Bad Request"}
	assert.Equal(t, "problem: 400 Bad Request", p2.Error())
}

func TestProblem_Is(t *testing.T) {
	p := &problem.Problem{Status: 429, Code: "rate_limited"}
	assert.True(t, errors.Is(p, &problem.Problem{Code: "rate_limited"}))
	assert.True(t, errors.Is(p, &problem.Problem{Status: 429}))
	assert.True(t, errors.Is(p, &problem.Problem{}))
	assert.False(t, errors.Is(p, &problem.Problem{Status: 400}))
	assert.False(t, errors.Is(p, &problem.Problem{Code: "other"}))
	assert.False(t, errors.Is(p, errors.New("nope")))
}

func TestDecode_ProblemJSON(t *testing.T) {
	body := `{"type":"about:blank","title":"Too Many Requests","status":429,"code":"rate_limited"}`
	resp := &http.Response{
		StatusCode: 429,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	p, err := problem.Decode(resp)
	require.NoError(t, err)
	assert.Equal(t, 429, p.Status)
	assert.Equal(t, "rate_limited", p.Code)
	assert.Equal(t, "Too Many Requests", p.Title)
}

func TestDecode_FillsStatusFromResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: 503,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		Body:       io.NopCloser(strings.NewReader(`{"title":"Service Unavailable"}`)),
	}
	p, err := problem.Decode(resp)
	require.NoError(t, err)
	assert.Equal(t, 503, p.Status)
}

func TestDecode_NotAProblem(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader("<html></html>")),
	}
	_, err := problem.Decode(resp)
	assert.ErrorIs(t, err, problem.ErrNotProblem)
}

func TestDecode_LeavesBodyOpen(t *testing.T) {
	rc := &trackCloser{Reader: strings.NewReader(`{"status":400,"code":"x"}`)}
	resp := &http.Response{
		StatusCode: 400,
		Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
		Body:       rc,
	}
	_, err := problem.Decode(resp)
	require.NoError(t, err)
	assert.False(t, rc.closed, "Decode must not close the response body")
}

// trackCloser records whether Close was called.
type trackCloser struct {
	io.Reader
	closed bool
}

func (t *trackCloser) Close() error { t.closed = true; return nil }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/problem -run 'TestProblem_|TestDecode_' -v`
Expected: FAIL — `undefined: problem.Decode` / `p.Error undefined`.

- [ ] **Step 3: Write the implementation**

Append to `web/problem/problem.go` (add `encoding/json`, `fmt`, `io` to the import block):

```go
// ErrNotProblem is returned by Decode when the response body is not a problem
// document. Match it with errors.Is.
var ErrNotProblem = errors.New("problem: not a problem+json response")

// maxProblemBytes caps the body Decode will read, guarding against a hostile or
// runaway upstream.
const maxProblemBytes = 1 << 20 // 1 MiB

// Error implements error with a single-line summary. The response body written by
// the responders is unaffected — this is for logs and errors.Is chains.
func (p *Problem) Error() string {
	if p.Code != "" {
		return fmt.Sprintf("problem: %d %s [%s]", p.Status, p.Title, p.Code)
	}
	return fmt.Sprintf("problem: %d %s", p.Status, p.Title)
}

// Is matches target by its non-zero fields: a *Problem target matches when
// (target.Status == 0 || target.Status == p.Status) &&
// (target.Code == "" || target.Code == p.Code). So errors.Is(err,
// &Problem{Code:"rate_limited"}) matches by code, &Problem{Status:429} by status,
// and &Problem{} any Problem. A non-*Problem target never matches.
func (p *Problem) Is(target error) bool {
	t, ok := target.(*Problem)
	if !ok {
		return false
	}
	if t.Status != 0 && t.Status != p.Status {
		return false
	}
	if t.Code != "" && t.Code != p.Code {
		return false
	}
	return true
}

// Decode reads an RFC 9457 problem+json response body into a *Problem. It caps the
// body at 1 MiB, fills Status from resp.StatusCode when the body omits it, and does
// NOT close resp.Body (the caller owns it). A body that is not a problem document
// returns ErrNotProblem.
func Decode(resp *http.Response) (*Problem, error) {
	if resp == nil || resp.Body == nil {
		return nil, ErrNotProblem
	}
	var p Problem
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxProblemBytes))
	if err := dec.Decode(&p); err != nil {
		return nil, ErrNotProblem
	}
	ct := resp.Header.Get("Content-Type")
	looksProblem := strings.Contains(ct, "application/problem+json") ||
		p.Status != 0 || p.Code != "" || p.Title != "" || p.Type != ""
	if !looksProblem {
		return nil, ErrNotProblem
	}
	if p.Status == 0 {
		p.Status = resp.StatusCode
	}
	return &p, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./web/problem -run 'TestProblem_|TestDecode_' -v`
Expected: PASS.

- [ ] **Step 5: Format, lint, test, commit**

```bash
just fmt ./web/problem/...
just lint
just test ./web/problem/...
git add web/problem/problem.go web/problem/problem_test.go
git commit -m "feat(web/problem): Decode response bodies and make *Problem an errors.Is-matchable error"
```

---

## Task 4: `web/request` — `ValidateFile` + `Accept`/`AcceptsJSON`

**Files:**
- Modify: `web/request/body.go` (add `FileOption`, `fileConfig`, `WithAllowedMIME`, `WithMaxFileSize`, `ValidateFile`)
- Create: `web/request/accept.go` (`Accept`, `AcceptsJSON` + helpers)
- Test: `web/request/body_files_test.go` (`package request_test` — append) and `web/request/accept_test.go` (`package request_test`)

**Interfaces:**
- Consumes: `request.File`/`request.Files`, `request.Error`/`Kind`/`Source`/`StatusCode`, `core/filetype.DetectReader`, `multipart.FileHeader`.
- Produces:
  - `type FileOption func(*fileConfig)` · `func WithAllowedMIME(...string) FileOption` · `func WithMaxFileSize(int64) FileOption`
  - `func ValidateFile(fh *multipart.FileHeader, opts ...FileOption) error`
  - `func Accept(r *http.Request, mediaType string) bool` · `func AcceptsJSON(r *http.Request) bool`

- [ ] **Step 1: Write the failing tests (ValidateFile)**

Append to `web/request/body_files_test.go` (reuses the existing package's multipart helper style; add `bytes`, `mime/multipart`, `net/http/httptest` imports if not present):

```go
// pngBytes is a minimal PNG magic-byte header for sniff tests.
var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

func multipartFileReq(t *testing.T, field, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	r := httptest.NewRequest(http.MethodPost, "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func TestValidateFile_AllowsSniffedPNG(t *testing.T) {
	r := multipartFileReq(t, "avatar", "a.png", pngBytes)
	fhs, err := request.Files(r, "avatar")
	require.NoError(t, err)
	require.Len(t, fhs, 1)
	assert.NoError(t, request.ValidateFile(fhs[0], request.WithAllowedMIME("image/png")))
}

func TestValidateFile_RejectsSpoofedType(t *testing.T) {
	// A .png filename whose bytes are plain text must be rejected by magic-byte sniff.
	r := multipartFileReq(t, "avatar", "evil.png", []byte("#!/bin/sh\necho hi\n"))
	fhs, err := request.Files(r, "avatar")
	require.NoError(t, err)
	err = request.ValidateFile(fhs[0], request.WithAllowedMIME("image/png"))
	require.Error(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, request.StatusCode(err))
}

func TestValidateFile_TooLarge(t *testing.T) {
	r := multipartFileReq(t, "avatar", "a.png", pngBytes)
	fhs, err := request.Files(r, "avatar")
	require.NoError(t, err)
	err = request.ValidateFile(fhs[0], request.WithMaxFileSize(4))
	require.Error(t, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, request.StatusCode(err))
}

func TestValidateFile_NilHeader(t *testing.T) {
	err := request.ValidateFile(nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, request.StatusCode(err))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./web/request -run TestValidateFile -v`
Expected: FAIL — `undefined: request.ValidateFile`.

- [ ] **Step 3: Implement `ValidateFile`**

Append to `web/request/body.go` (add `github.com/dmitrymomot/forge/core/filetype` to the import block):

```go
// FileOption configures ValidateFile.
type FileOption func(*fileConfig)

type fileConfig struct {
	allowedMIME []string
	maxSize     int64
}

// WithAllowedMIME restricts uploads to these magic-byte-sniffed MIME types (e.g.
// "image/png", "application/pdf"). With no allowlist the MIME is not checked.
func WithAllowedMIME(mimes ...string) FileOption {
	return func(c *fileConfig) { c.allowedMIME = mimes }
}

// WithMaxFileSize rejects uploads whose declared size exceeds n bytes. With no
// limit the size is not checked.
func WithMaxFileSize(n int64) FileOption {
	return func(c *fileConfig) { c.maxSize = n }
}

// ValidateFile validates an uploaded file by its magic bytes (core/filetype),
// deliberately ignoring the client-declared Content-Type, plus an optional size
// cap. It returns a *Error: KindTooLarge for an oversize file,
// KindUnsupportedMediaType for a disallowed/undetectable type, KindMissing for a
// nil header; nil on success. Consumers with multiple files loop over Files().
func ValidateFile(fh *multipart.FileHeader, opts ...FileOption) error {
	if fh == nil {
		return &Error{Source: SourceForm, Kind: KindMissing}
	}
	var c fileConfig
	for _, o := range opts {
		o(&c)
	}
	if c.maxSize > 0 && fh.Size > c.maxSize {
		return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindTooLarge}
	}
	if len(c.allowedMIME) == 0 {
		return nil
	}
	f, err := fh.Open()
	if err != nil {
		return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindUnsupportedMediaType, Err: err}
	}
	defer func() { _ = f.Close() }()
	typ, _, err := filetype.DetectReader(f)
	if err != nil {
		return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindUnsupportedMediaType, Err: err}
	}
	for _, m := range c.allowedMIME {
		if typ.MIME == m {
			return nil
		}
	}
	return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindUnsupportedMediaType}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./web/request -run TestValidateFile -v`
Expected: PASS.

- [ ] **Step 5: Write the failing tests (Accept)**

Create `web/request/accept_test.go`:

```go
package request_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/request"
	"github.com/stretchr/testify/assert"
)

func TestAccept(t *testing.T) {
	cases := []struct {
		accept string
		media  string
		want   bool
	}{
		{"application/json", "application/json", true},
		{"*/*", "application/json", true},
		{"text/*", "text/html", true},
		{"text/*", "application/json", false},
		{"application/json;q=0", "application/json", false},
		{"", "application/json", true},
		{"text/html, application/json;q=0.9", "application/json", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		assert.Equalf(t, c.want, request.Accept(r, c.media), "Accept %q media %q", c.accept, c.media)
	}
}

func TestAcceptsJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept", "application/json")
	assert.True(t, request.AcceptsJSON(r))
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./web/request -run 'TestAccept' -v`
Expected: FAIL — `undefined: request.Accept`.

- [ ] **Step 7: Implement `Accept`**

Create `web/request/accept.go`:

```go
package request

import (
	"net/http"
	"strconv"
	"strings"
)

// Accept reports whether the request's Accept header admits mediaType, honoring
// "*/*", "type/*", and explicit "q=0" rejections. An absent or empty Accept header
// admits everything (returns true), per RFC 9110.
func Accept(r *http.Request, mediaType string) bool {
	header := r.Header.Get("Accept")
	if strings.TrimSpace(header) == "" {
		return true
	}
	want := strings.SplitN(strings.ToLower(strings.TrimSpace(mediaType)), "/", 2)
	if len(want) != 2 {
		return false
	}
	best := -1.0
	for _, part := range strings.Split(header, ",") {
		rng, q := parseAcceptPart(part)
		if rng == "" {
			continue
		}
		if acceptMatches(rng, want) && q > best {
			best = q
		}
	}
	return best > 0
}

// AcceptsJSON reports whether the request admits application/json.
func AcceptsJSON(r *http.Request) bool { return Accept(r, "application/json") }

// parseAcceptPart splits one Accept list element into its lowercased media range
// and q-value (default 1.0).
func parseAcceptPart(part string) (string, float64) {
	fields := strings.Split(part, ";")
	rng := strings.ToLower(strings.TrimSpace(fields[0]))
	q := 1.0
	for _, p := range fields[1:] {
		if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				q = f
			}
		}
	}
	return rng, q
}

// acceptMatches reports whether a media range admits the wanted type/subtype.
func acceptMatches(rng string, want []string) bool {
	if rng == "*/*" {
		return true
	}
	rp := strings.SplitN(rng, "/", 2)
	if len(rp) != 2 {
		return false
	}
	if rp[0] != want[0] {
		return false
	}
	return rp[1] == "*" || rp[1] == want[1]
}
```

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./web/request -run 'TestAccept' -v`
Expected: PASS.

- [ ] **Step 9: Format, lint, test, commit**

```bash
just fmt ./web/request/...
just lint
just test ./web/request/...
git add web/request/body.go web/request/accept.go web/request/body_files_test.go web/request/accept_test.go
git commit -m "feat(web/request): magic-byte ValidateFile and Accept/AcceptsJSON negotiation"
```

---

## Task 5: `web/middleware` — `When` / `Skip`

**Files:**
- Modify: `web/middleware/middleware.go`
- Test: `web/middleware/middleware_test.go` (`package middleware_test` — append)

**Interfaces:**
- Consumes: `middleware.Middleware`, `middleware.Wrap`.
- Produces: `func When(pred func(*http.Request) bool, mw Middleware) Middleware` · `func Skip(pred func(*http.Request) bool, mw Middleware) Middleware`

- [ ] **Step 1: Write the failing tests**

Append to `web/middleware/middleware_test.go`:

```go
func markHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Marked", "1")
		next.ServeHTTP(w, r)
	})
}

func TestWhen_AppliesOnlyWhenPredicateTrue(t *testing.T) {
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.When(func(r *http.Request) bool { return r.URL.Path == "/on" }, markHeader),
	)
	on := httptest.NewRecorder()
	h.ServeHTTP(on, httptest.NewRequest(http.MethodGet, "/on", nil))
	assert.Equal(t, "1", on.Header().Get("X-Marked"))

	off := httptest.NewRecorder()
	h.ServeHTTP(off, httptest.NewRequest(http.MethodGet, "/off", nil))
	assert.Empty(t, off.Header().Get("X-Marked"))
}

func TestSkip_IsInverseOfWhen(t *testing.T) {
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.Skip(func(r *http.Request) bool { return r.URL.Path == "/skip" }, markHeader),
	)
	skip := httptest.NewRecorder()
	h.ServeHTTP(skip, httptest.NewRequest(http.MethodGet, "/skip", nil))
	assert.Empty(t, skip.Header().Get("X-Marked"))

	apply := httptest.NewRecorder()
	h.ServeHTTP(apply, httptest.NewRequest(http.MethodGet, "/other", nil))
	assert.Equal(t, "1", apply.Header().Get("X-Marked"))
}

func TestWhen_BuildsMiddlewareOnce(t *testing.T) {
	builds := 0
	mw := func(next http.Handler) http.Handler {
		builds++
		return next
	}
	h := middleware.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		middleware.When(func(*http.Request) bool { return true }, mw),
	)
	for range 3 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	assert.Equal(t, 1, builds)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./web/middleware -run 'TestWhen|TestSkip' -v`
Expected: FAIL — `undefined: middleware.When`.

- [ ] **Step 3: Write the implementation**

Append to `web/middleware/middleware.go`:

```go
// When returns a Middleware that applies mw only to requests for which pred
// returns true; other requests pass to the next handler untouched. mw is built
// once per next, so a stateful middleware is constructed a single time.
func When(pred func(*http.Request) bool, mw Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pred(r) {
				wrapped.ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// Skip is the inverse of When: mw applies unless pred returns true.
func Skip(pred func(*http.Request) bool, mw Middleware) Middleware {
	return When(func(r *http.Request) bool { return !pred(r) }, mw)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./web/middleware -run 'TestWhen|TestSkip' -v`
Expected: PASS.

- [ ] **Step 5: Format, lint, test, commit**

```bash
just fmt ./web/middleware/...
just lint
just test ./web/middleware/...
git add web/middleware/middleware.go web/middleware/middleware_test.go
git commit -m "feat(web/middleware): When/Skip conditional middleware combinators"
```

---

## Task 6: `ops/supervisor` — `WithForceQuit`

**Files:**
- Modify: `ops/supervisor/context.go` (variadic `NewContext` + force-quit path)
- Modify: `ops/supervisor/options.go` (add `ContextOption`, `contextConfig`, `WithForceQuit`)
- Test: `ops/supervisor/context_test.go` (`package supervisor_test` — append)

**Interfaces:**
- Consumes: existing `signal.NotifyContext` behavior.
- Produces: `type ContextOption func(*contextConfig)` · `func WithForceQuit() ContextOption` · `func NewContext(opts ...ContextOption) (context.Context, context.CancelFunc)`

- [ ] **Step 1: Write the failing tests**

Append to `ops/supervisor/context_test.go` (add `os`, `os/exec`, `syscall`, `time` imports as needed):

```go
func TestNewContext_ForceQuit_FirstSignalCancels(t *testing.T) {
	ctx, stop := supervisor.NewContext(supervisor.WithForceQuit())
	defer stop()
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGTERM))
	select {
	case <-ctx.Done():
		assert.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("context not cancelled after first signal")
	}
}

func TestNewContext_ForceQuit_SecondSignalExits(t *testing.T) {
	if os.Getenv("FORGE_FORCEQUIT_CHILD") == "1" {
		_, stop := supervisor.NewContext(supervisor.WithForceQuit())
		defer stop()
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		time.Sleep(2 * time.Second) // parent expects exit(130) before this returns
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestNewContext_ForceQuit_SecondSignalExits")
	cmd.Env = append(os.Environ(), "FORGE_FORCEQUIT_CHILD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 130, ee.ExitCode())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./ops/supervisor -run TestNewContext_ForceQuit -v`
Expected: FAIL — `undefined: supervisor.WithForceQuit`.

- [ ] **Step 3: Add the option**

Append to `ops/supervisor/options.go`:

```go
// ContextOption configures NewContext.
type ContextOption func(*contextConfig)

type contextConfig struct {
	forceQuit bool
}

// WithForceQuit makes the second SIGINT/SIGTERM force an immediate os.Exit(130)
// instead of being ignored. The first signal still cancels the returned context for
// graceful drain; the second is the impatient-operator escape hatch. os.Exit
// bypasses deferred cleanup by design.
func WithForceQuit() ContextOption {
	return func(c *contextConfig) { c.forceQuit = true }
}
```

- [ ] **Step 4: Rewrite `NewContext`**

Replace the body of `ops/supervisor/context.go` with (keep `package supervisor`):

```go
package supervisor

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// forceQuitCode is the process exit code used when WithForceQuit triggers on the
// second signal (128 + SIGINT, the conventional forced-interrupt code).
const forceQuitCode = 130

// NewContext returns a context cancelled on the first SIGINT or SIGTERM. Call the
// returned CancelFunc (typically deferred in main) to release the signal handler.
// It is single-shot by default: after the first signal further signals are not
// handled. With WithForceQuit, the first signal still cancels the context for a
// graceful drain and a second signal forces os.Exit(130).
func NewContext(opts ...ContextOption) (context.Context, context.CancelFunc) {
	var cfg contextConfig
	for _, o := range opts {
		o(&cfg)
	}
	if !cfg.forceQuit {
		return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
			return
		}
		select {
		case <-ch:
			os.Exit(forceQuitCode)
		case <-ctx.Done():
		}
	}()
	stop := func() {
		signal.Stop(ch)
		cancel()
	}
	return ctx, stop
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./ops/supervisor -run TestNewContext -v`
Expected: PASS (existing `TestNewContext_*` still pass — the no-arg call still compiles; the new force-quit tests pass, including the subprocess exit-130 assertion).

- [ ] **Step 6: Format, lint, test, commit**

```bash
just fmt ./ops/supervisor/...
just lint
just test ./ops/supervisor/...
git add ops/supervisor/context.go ops/supervisor/options.go ops/supervisor/context_test.go
git commit -m "feat(ops/supervisor): WithForceQuit second-signal os.Exit on NewContext"
```

---

## Task 7: `ops/logger` — recording test handler

**Files:**
- Create: `ops/logger/record.go`
- Test: `ops/logger/record_test.go` (`package logger_test` — black-box, since the whole API is exported)

**Interfaces:**
- Consumes: `log/slog`.
- Produces: `type Record struct{…}` · `type Recorder struct{…}` · `func NewRecorder() (*slog.Logger, *Recorder)` · `func (*Recorder) Records() []Record` · `Len()` · `Reset()` · `Contains(slog.Level, string) bool`

- [ ] **Step 1: Write the failing tests**

Create `ops/logger/record_test.go`:

```go
package logger_test

import (
	"log/slog"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecorder_CapturesLevelAndMessage(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.Info("hello")
	log.Error("boom")
	assert.Equal(t, 2, rec.Len())
	assert.True(t, rec.Contains(slog.LevelInfo, "hello"))
	assert.True(t, rec.Contains(slog.LevelError, "boom"))
	assert.False(t, rec.Contains(slog.LevelWarn, "hello"))
}

func TestRecorder_FlattensGroupedAttrs(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.Info("req", slog.Group("http", slog.Int("status", 200)))
	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, int64(200), recs[0].Attrs["http.status"])
}

func TestRecorder_WithGroupAndWithAttrs(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.With(slog.String("svc", "api")).WithGroup("db").Info("query", slog.Int("rows", 3))
	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "api", recs[0].Attrs["svc"])
	assert.Equal(t, int64(3), recs[0].Attrs["db.rows"])
}

func TestRecorder_Reset(t *testing.T) {
	log, rec := logger.NewRecorder()
	log.Info("x")
	rec.Reset()
	assert.Equal(t, 0, rec.Len())
	assert.Empty(t, rec.Records())
}

func TestRecorder_ConcurrentSafe(t *testing.T) {
	log, rec := logger.NewRecorder()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("m", slog.Int("i", i))
		}()
	}
	wg.Wait()
	assert.Equal(t, 50, rec.Len())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./ops/logger -run TestRecorder -v`
Expected: FAIL — `undefined: logger.NewRecorder`.

- [ ] **Step 3: Write the implementation**

Create `ops/logger/record.go`:

```go
package logger

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Record is one captured log record with attributes flattened to dotted keys (a
// WithGroup("http") + Int("status", …) attr becomes "http.status").
type Record struct {
	Time    time.Time
	Attrs   map[string]any
	Message string
	Level   slog.Level
}

// Recorder is a concurrency-safe slog.Handler sink that captures records for test
// assertions. It is the seam owner's test double — there is no central fakes
// package. Construct it with NewRecorder.
type Recorder struct {
	mu      sync.Mutex
	records []Record
}

// NewRecorder returns a *slog.Logger writing into the returned *Recorder.
func NewRecorder() (*slog.Logger, *Recorder) {
	r := &Recorder{}
	return slog.New(&recordHandler{rec: r}), r
}

// Records returns a snapshot copy of the captured records.
func (r *Recorder) Records() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, len(r.records))
	copy(out, r.records)
	return out
}

// Len returns the number of captured records.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// Reset discards all captured records.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = nil
}

// Contains reports whether any captured record has the given level and message.
func (r *Recorder) Contains(level slog.Level, msg string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		if rec.Level == level && rec.Message == msg {
			return true
		}
	}
	return false
}

func (r *Recorder) append(rec Record) {
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
}

// recordHandler is the slog.Handler that flattens attributes and appends to the
// shared Recorder. WithAttrs/WithGroup return fresh handlers (no mutation).
type recordHandler struct {
	rec    *Recorder
	attrs  map[string]any // pre-bound (WithAttrs) attrs, already flattened
	prefix string         // dotted group prefix, e.g. "http."
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordHandler) Handle(_ context.Context, rec slog.Record) error {
	flat := make(map[string]any, len(h.attrs)+rec.NumAttrs())
	for k, v := range h.attrs {
		flat[k] = v
	}
	rec.Attrs(func(a slog.Attr) bool {
		flattenAttr(flat, h.prefix, a)
		return true
	})
	h.rec.append(Record{
		Time:    rec.Time,
		Level:   rec.Level,
		Message: rec.Message,
		Attrs:   flat,
	})
	return nil
}

func (h *recordHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	for _, a := range attrs {
		flattenAttr(next.attrs, h.prefix, a)
	}
	return next
}

func (h *recordHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.clone()
	next.prefix = h.prefix + name + "."
	return next
}

func (h *recordHandler) clone() *recordHandler {
	attrs := make(map[string]any, len(h.attrs))
	for k, v := range h.attrs {
		attrs[k] = v
	}
	return &recordHandler{rec: h.rec, prefix: h.prefix, attrs: attrs}
}

// flattenAttr writes a into dst under prefix, recursing into group values so nested
// keys become dotted (prefix + group + "." + key).
func flattenAttr(dst map[string]any, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		gp := prefix
		if a.Key != "" {
			gp = prefix + a.Key + "."
		}
		for _, ga := range a.Value.Group() {
			flattenAttr(dst, gp, ga)
		}
		return
	}
	dst[prefix+a.Key] = a.Value.Any()
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./ops/logger -run TestRecorder -race -v`
Expected: PASS (including the `-race` concurrent test).

- [ ] **Step 5: Format, lint, test, commit**

```bash
just fmt ./ops/logger/...
just lint
just test ./ops/logger/...
git add ops/logger/record.go ops/logger/record_test.go
git commit -m "feat(ops/logger): concurrency-safe recording slog test handler"
```

---

## Task 8: `web/hostrouter` — DNS-rebinding doc note (doc-only)

**Files:**
- Modify: `web/hostrouter/doc.go`, `web/hostrouter/hostrouter.go` (New comment), `web/hostrouter/options.go` (WithFallback comment)
- Test: none (existing `TestRouter_DefaultFallbackIs404` already covers the behavior)

**Interfaces:** No API change.

- [ ] **Step 1: Add the security note to `doc.go`**

In `web/hostrouter/doc.go`, insert this paragraph after the existing wildcard/misconfiguration paragraph (before the `# Usage` heading):

```go
// # Security: default-deny is DNS-rebinding protection
//
// Unmatched hosts fall through to the fallback (http.NotFoundHandler(), 404, by
// default). This default-deny is a DNS-rebinding defense: a handler is reachable
// only for explicitly registered Host values, so an attacker who points their own
// domain at your IP reaches the fallback, not a real handler. Do not install a
// WithFallback that serves sensitive handlers without validating the Host itself.
```

- [ ] **Step 2: Add a note to the `New` comment**

In `web/hostrouter/hostrouter.go`, extend the `New` doc comment. Change:

```go
// New builds a Router from options applied in order. With no WithHost options every
// request is served by the fallback (HTTP 404 unless WithFallback overrides it).
// New panics on any invalid registration. It does no I/O.
```

to:

```go
// New builds a Router from options applied in order. With no WithHost options every
// request is served by the fallback (HTTP 404 unless WithFallback overrides it).
// The default 404-for-unmatched-hosts is a deliberate default-deny that protects
// against DNS rebinding. New panics on any invalid registration. It does no I/O.
```

- [ ] **Step 3: Add a warning to the `WithFallback` comment**

In `web/hostrouter/options.go`, change:

```go
// WithFallback sets the handler for unmatched hosts. The default is
// http.NotFoundHandler() (404). It panics (ErrNilHandler) if h is nil. Last wins.
```

to:

```go
// WithFallback sets the handler for unmatched hosts. The default is
// http.NotFoundHandler() (404), a default-deny that guards against DNS rebinding;
// overriding it to serve real content means unmatched (possibly rebound) hosts
// reach h, so validate the Host inside h if it exposes anything sensitive. It
// panics (ErrNilHandler) if h is nil. Last wins.
```

- [ ] **Step 4: Verify docs build and the existing test still passes**

```bash
just fmt ./web/hostrouter/...
just lint
just test ./web/hostrouter/...
```

Expected: all pass (no behavior change; `go build` and the existing 404 test are green).

- [ ] **Step 5: Commit**

```bash
git add web/hostrouter/doc.go web/hostrouter/hostrouter.go web/hostrouter/options.go
git commit -m "docs(web/hostrouter): note default-deny as DNS-rebinding protection"
```

---

## Task 9: `docs/packages.md` — prune the shipped-work table

**Files:**
- Modify: `docs/packages.md` (the "Shipped-package work items" table)

**Interfaces:** No code change.

- [ ] **Step 1: Replace the table body**

In `docs/packages.md`, find the table under `## Shipped-package work items` and replace its full body (all rows) so only the deferred htmx row remains:

Replace:

```markdown
| Package | Addition |
|---|---|
| `core/random` | `String(n, alphabet)`, `DigitCode(n)` (bias-free; `otp` needs it) |
| `core/id` | Stripe-style `Prefixed` IDs with prefix-validating Parse |
| `web/request` | `ValidateFile` (MIME allowlist + `filetype` sniff over File/Files); `Accept`/`AcceptsJSON` helpers |
| `web/middleware` | `Skip`/`When(predicate)` combinator |
| `web/problem` | Machine-readable `Code` field; `Decode(*http.Response)`; `errors.Is` on (Status, Code) |
| `web/htmx` | SSE `SendComponent` bridge (moved out of sse's scope) |
| `resilience/circuitbreaker` | Keyed `Group` (lazy per-key breakers); server-side HTTP middleware adapter (503 + Retry-After) |
| `resilience/retry` | Honor `interface{ RetryAfter() time.Duration }` errors as the delay floor |
| `resilience/cache` | Atomic SetNX in the Store contract |
| `ops/supervisor` | `WithForceQuit` second-signal option on `NewContext` |
| `ops/logger` | Recording slog test handler |
| `web/hostrouter` | Doc note: unknown hosts default-deny = DNS-rebinding protection |
```

with:

```markdown
| Package | Addition |
|---|---|
| `web/htmx` | SSE `SendComponent` bridge — deferred to the realtime/sse wave |
```

- [ ] **Step 2: Verify and commit**

```bash
git add docs/packages.md
git commit -m "docs(packages): drop completed Wave 1 unblocker rows; defer htmx SendComponent"
```

---

## Final Verification (bundle gate)

After all nine tasks are committed, run the full suite once to confirm the bundle is coherent:

- [ ] **Run the whole check**

```bash
just check   # = just fmt ./... && just lint && just test ./...
```

Expected: `go vet`, `go build`, `golangci-lint`, `nilaway`, `betteralign`, `modernize` all clean; the full test suite green under `-race`.

- [ ] **Confirm git state**

```bash
git status          # clean tree
git log --oneline -10   # nine feature/docs commits present
```

---

## Self-Review (author notes)

- **Spec coverage:** Each of the eight spec items maps to a task (1–8); the packages.md housekeeping maps to Task 9. The spec's "deferred" items (htmx SendComponent, weighted random) are intentionally absent.
- **Type consistency:** `filetype.Type.MIME`, `request.Error{Source,Key,Kind,Err}`, `request.Kind{TooLarge,UnsupportedMediaType,Missing}`, `request.StatusCode`, `id.ErrMalformed`, `Short.String`/`ParseShort`, `slog.Handler` method set, and `middleware.Middleware`/`Wrap` are all used exactly as they exist in the codebase (verified during recon).
- **Independence:** No task consumes a symbol produced by another task — order is free; safe for parallel subagent execution.
