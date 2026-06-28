# P1 Crypto Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the P1 crypto layer — nine flat packages (`subtlex`, `hashx`, `redact`, `kdf`, `keyset`, `sign`, `password`, `secret`, `token`) — plus the two minimal P0 seams they need (`clock`, `randx`), as stdlib-first cryptographic building blocks.

**Architecture:** Eleven flat top-level packages in five dependency waves. Each is a small, single-responsibility package following forge DNA: stateless free-funcs or `New(...Option)`/`New[T](...Option)` returning a small value, `errors.Is` sentinels, black-box tests. Key wiring is "raw key primary, keyset opt-in" (`New(key)` for the single-key case, `FromKeyset(ks)` for rotation; the key version rides in the encoded output). `token` is generic-primary (`Codec[T]` with internal JSON). The only external dep is `golang.org/x/crypto`, confined to `password`/`kdf`/`secret`.

**Tech Stack:** Go 1.26, stdlib `crypto/*` + `crypto/hkdf` (Go 1.24+), `log/slog`, `iter`; `golang.org/x/crypto` (argon2, bcrypt, chacha20poly1305) already vendored as indirect; testify for tests.

## Global Constraints

Every task implicitly includes these (copied verbatim from the spec / project rules):

- **Go 1.26**; module path `github.com/dmitrymomot/forge`.
- **Flat top-level packages** — `clock/`, `randx/`, `subtlex/`, … No `pkg/` prefix, no nesting.
- **Options are `type Option func(*config)` — never builders.** Invalid option values accumulate into `c.errs` and are returned (joined) by the constructor.
- **Errors are `errors.Is`-matchable single-line sentinels**: `var ErrX = errors.New("pkg: message")`. No stacks, no embedded blobs. Wrap with `fmt.Errorf("%w: …", ErrX, …)`.
- **Black-box tests only** — test package is `<pkg>_test`; use `github.com/stretchr/testify/assert` + `require`.
- **No package imports `supervisor` or `logger`.** Intra-layer imports only, per the wave DAG.
- **Constant-time comparison via `subtlex`** for every secret/MAC/tag/hash compare — never `==` or `bytes.Equal`.
- **Public methods never return unexported types.**
- **`x/crypto` is the only external dep**, imported only by `password`, `kdf`, `secret`.
- Verification recipe per package: `go test -race -cover ./<pkg>/`. Full gate at the end: `just check` (fmt + lint + test). `just` recipes: `test path='./...'`, `lint`, `fmt`, `check: fmt lint test`.

## File Structure

One directory per package; each contains the impl file(s), `doc.go`, `errors.go` (where there are sentinels), `options.go` (where there are options), and a black-box `_test.go`.

```
clock/      clock.go  doc.go  clock_test.go
randx/      randx.go  doc.go  randx_test.go
subtlex/    subtlex.go  doc.go  subtlex_test.go
hashx/      hashx.go  doc.go  hashx_test.go
redact/     redact.go  doc.go  redact_test.go
kdf/        kdf.go  errors.go  doc.go  kdf_test.go
keyset/     keyset.go  errors.go  options.go  doc.go  keyset_test.go
sign/       sign.go  errors.go  options.go  doc.go  sign_test.go
password/   password.go  errors.go  options.go  doc.go  password_test.go
secret/     secret.go  errors.go  options.go  doc.go  secret_test.go
token/      token.go  errors.go  options.go  doc.go  token_test.go
go.mod      (x/crypto promoted indirect → direct by `go mod tidy` after Task 6)
```

Wave / dependency order (each task depends only on earlier ones):

| Wave | Tasks | Packages |
|---|---|---|
| 0 | 1, 2 | `clock`, `randx` |
| 1 | 3, 4, 5, 6 | `subtlex`, `hashx`, `redact`, `kdf` |
| 2 | 7, 8, 9 | `keyset`, `sign`, `password` |
| 3 | 10 | `secret` |
| 4 | 11 | `token` |
| — | 12 | full-tree verification (`just check`) |

---

## Task 1: `clock` — testable time seam

**Files:**
- Create: `clock/clock.go`
- Create: `clock/doc.go`
- Test: `clock/clock_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces: `type Clock interface { Now() time.Time }`; `func System() Clock`; `type Mock struct{…}`; `func NewMock(t time.Time) *Mock`; `(*Mock).Now() time.Time`; `(*Mock).Set(t time.Time)`; `(*Mock).Advance(d time.Duration)`.

- [ ] **Step 1: Write the failing test**

`clock/clock_test.go`:
```go
package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/clock"
)

func TestSystem_NowIsCurrent(t *testing.T) {
	before := time.Now()
	got := clock.System().Now()
	after := time.Now()
	assert.False(t, got.Before(before))
	assert.False(t, got.After(after))
}

func TestMock_Now(t *testing.T) {
	base := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	m := clock.NewMock(base)
	assert.Equal(t, base, m.Now())
}

func TestMock_Set(t *testing.T) {
	m := clock.NewMock(time.Unix(0, 0))
	next := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	m.Set(next)
	assert.Equal(t, next, m.Now())
}

func TestMock_Advance(t *testing.T) {
	base := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	m := clock.NewMock(base)
	m.Advance(90 * time.Minute)
	assert.Equal(t, base.Add(90*time.Minute), m.Now())
}

func TestMock_ImplementsClock(t *testing.T) {
	var _ clock.Clock = clock.NewMock(time.Now())
	var _ clock.Clock = clock.System()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./clock/`
Expected: FAIL — `undefined: clock.System` / `clock.NewMock`.

- [ ] **Step 3: Write the implementation**

`clock/clock.go`:
```go
package clock

import (
	"sync"
	"time"
)

// Clock reports the current time. Production code depends on this interface rather
// than calling time.Now directly, so time-dependent logic is deterministic in tests.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// System returns the production clock backed by time.Now.
func System() Clock { return systemClock{} }

// Mock is a goroutine-safe, controllable Clock for tests.
type Mock struct {
	mu sync.Mutex
	t  time.Time
}

// NewMock returns a Mock fixed at t.
func NewMock(t time.Time) *Mock { return &Mock{t: t} }

// Now returns the mock's current time.
func (m *Mock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.t
}

// Set replaces the mock's current time.
func (m *Mock) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = t
}

// Advance moves the mock's current time forward by d.
func (m *Mock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = m.t.Add(d)
}
```

`clock/doc.go`:
```go
// Package clock is the framework's testable time seam. Production code accepts a
// Clock and calls Now instead of time.Now, so expiry and scheduling logic can be
// driven deterministically in tests via Mock.
//
//	type svc struct{ clk clock.Clock }
//	func newSvc() *svc { return &svc{clk: clock.System()} }
//
//	// in a test:
//	m := clock.NewMock(time.Unix(0, 0))
//	s := &svc{clk: m}
//	m.Advance(time.Hour) // s now sees time one hour later
//
// Only Now is provided today. Timer/ticker helpers are deferred until the async
// layer needs them; adding them later is additive.
package clock
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./clock/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add clock/
git commit -m "feat(clock): add testable time seam (Clock, System, Mock)"
```

---

## Task 2: `randx` — secure entropy

**Files:**
- Create: `randx/randx.go`
- Create: `randx/doc.go`
- Test: `randx/randx_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces: `func Bytes(n int) []byte`; `func Read(p []byte) error`; `func Hex(n int) string`; `func URLSafe(n int) string`; `func Int(max int) int`.

- [ ] **Step 1: Write the failing test**

`randx/randx_test.go`:
```go
package randx_test

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/randx"
)

func TestBytes_LengthAndUniqueness(t *testing.T) {
	a := randx.Bytes(32)
	b := randx.Bytes(32)
	assert.Len(t, a, 32)
	assert.Len(t, b, 32)
	assert.NotEqual(t, a, b) // astronomically unlikely to collide
}

func TestRead(t *testing.T) {
	p := make([]byte, 16)
	require.NoError(t, randx.Read(p))
	assert.NotEqual(t, make([]byte, 16), p)
}

func TestHex(t *testing.T) {
	s := randx.Hex(8)
	assert.Len(t, s, 16) // 2 hex chars per byte
	_, err := hex.DecodeString(s)
	require.NoError(t, err)
}

func TestURLSafe(t *testing.T) {
	s := randx.URLSafe(16)
	decoded, err := base64.RawURLEncoding.DecodeString(s)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
}

func TestInt_InRange(t *testing.T) {
	for range 1000 {
		v := randx.Int(10)
		assert.GreaterOrEqual(t, v, 0)
		assert.Less(t, v, 10)
	}
}

func TestInt_PanicsOnNonPositive(t *testing.T) {
	assert.Panics(t, func() { randx.Int(0) })
	assert.Panics(t, func() { randx.Int(-5) })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./randx/`
Expected: FAIL — `undefined: randx.Bytes`.

- [ ] **Step 3: Write the implementation**

`randx/randx.go`:
```go
package randx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
)

// Read fills p with cryptographically secure random bytes. It is the error-returning
// escape hatch; most callers want Bytes.
func Read(p []byte) error {
	if _, err := rand.Read(p); err != nil {
		return fmt.Errorf("randx: read: %w", err)
	}
	return nil
}

// Bytes returns n cryptographically secure random bytes. It panics only if crypto/rand
// fails, which indicates a broken OS RNG — an unrecoverable condition.
func Bytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("randx: crypto/rand failed: %w", err))
	}
	return b
}

// Hex returns the hex encoding of n random bytes (2n characters).
func Hex(n int) string { return hex.EncodeToString(Bytes(n)) }

// URLSafe returns the unpadded base64url encoding of n random bytes.
func URLSafe(n int) string { return base64.RawURLEncoding.EncodeToString(Bytes(n)) }

// Int returns an unbiased random integer in [0, max). It panics if max <= 0.
func Int(max int) int {
	if max <= 0 {
		panic("randx: Int max must be > 0")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(fmt.Errorf("randx: crypto/rand failed: %w", err))
	}
	return int(v.Int64())
}
```

`randx/doc.go`:
```go
// Package randx provides small, safe helpers over crypto/rand for secure entropy:
// token nonces, salts, and opaque identifiers.
//
//	salt := randx.Bytes(16)
//	id := randx.URLSafe(24)
//
// Bytes/Hex/URLSafe/Int panic only on a crypto/rand failure, which means the OS RNG
// is broken and the program cannot safely continue. Use Read for an error-returning
// variant. Int is unbiased (rejection sampling via crypto/rand.Int).
package randx
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./randx/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add randx/
git commit -m "feat(randx): add secure entropy helpers over crypto/rand"
```

---

## Task 3: `subtlex` — constant-time, length-safe comparison

**Files:**
- Create: `subtlex/subtlex.go`
- Create: `subtlex/doc.go`
- Test: `subtlex/subtlex_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces: `func BytesEqual(a, b []byte) bool`; `func StringEqual(a, b string) bool`.

- [ ] **Step 1: Write the failing test**

`subtlex/subtlex_test.go`:
```go
package subtlex_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/subtlex"
)

func TestBytesEqual(t *testing.T) {
	assert.True(t, subtlex.BytesEqual([]byte("secret"), []byte("secret")))
	assert.False(t, subtlex.BytesEqual([]byte("secret"), []byte("Secret")))
	assert.False(t, subtlex.BytesEqual([]byte("secret"), []byte("secretX"))) // different length
	assert.True(t, subtlex.BytesEqual(nil, nil))
	assert.True(t, subtlex.BytesEqual([]byte{}, []byte{}))
	assert.False(t, subtlex.BytesEqual([]byte("x"), nil))
}

func TestStringEqual(t *testing.T) {
	assert.True(t, subtlex.StringEqual("token-abc", "token-abc"))
	assert.False(t, subtlex.StringEqual("token-abc", "token-abd"))
	assert.False(t, subtlex.StringEqual("short", "longer-value"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./subtlex/`
Expected: FAIL — `undefined: subtlex.BytesEqual`.

- [ ] **Step 3: Write the implementation**

`subtlex/subtlex.go`:
```go
package subtlex

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
)

// BytesEqual reports whether a and b are equal in constant time, without leaking their
// length via an early return. Each side is reduced to a fixed-length HMAC-SHA256 digest
// under a fresh per-call key (the "double HMAC" pattern), then compared with
// crypto/subtle. Comparing values of different lengths therefore reveals nothing through
// timing.
func BytesEqual(a, b []byte) bool {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Unreachable in practice; fall back to keyless digests, still constant-time.
		ha := sha256.Sum256(a)
		hb := sha256.Sum256(b)
		return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
	}
	return subtle.ConstantTimeCompare(mac(key, a), mac(key, b)) == 1
}

// StringEqual is BytesEqual for strings.
func StringEqual(a, b string) bool { return BytesEqual([]byte(a), []byte(b)) }

func mac(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}
```

`subtlex/doc.go`:
```go
// Package subtlex provides constant-time, length-safe equality for secrets — MACs,
// tokens, API keys, password hashes — removing the timing-attack footgun of == and
// bytes.Equal. It is the comparison primitive the rest of the crypto layer builds on.
//
//	if subtlex.StringEqual(presentedAPIKey, storedAPIKey) {
//		// authenticated
//	}
package subtlex
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./subtlex/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add subtlex/
git commit -m "feat(subtlex): add constant-time length-safe comparison"
```

---

## Task 4: `hashx` — digest convenience

**Files:**
- Create: `hashx/hashx.go`
- Create: `hashx/doc.go`
- Test: `hashx/hashx_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces: `SHA256([]byte) []byte`; `SHA256Hex([]byte) string`; `SHA256Base64([]byte) string`; `SHA512([]byte) []byte`; `SHA512Hex([]byte) string`; `HMACSHA256(key, msg []byte) []byte`; `HMACSHA256Hex(key, msg []byte) string`; `FileSHA256(path string) (string, error)`.

- [ ] **Step 1: Write the failing test**

`hashx/hashx_test.go`:
```go
package hashx_test

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/hashx"
)

// SHA-256("abc") — FIPS 180-2 example digest.
const sha256abcHex = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

func TestSHA256_KnownAnswer(t *testing.T) {
	assert.Equal(t, sha256abcHex, hashx.SHA256Hex([]byte("abc")))
	assert.Len(t, hashx.SHA256([]byte("abc")), 32)
}

func TestSHA256Base64(t *testing.T) {
	// Derive the expected value from the known digest rather than hardcoding a
	// base64 literal (avoids transcription errors).
	digest, err := hex.DecodeString(sha256abcHex)
	require.NoError(t, err)
	want := base64.RawStdEncoding.EncodeToString(digest)
	assert.Equal(t, want, hashx.SHA256Base64([]byte("abc")))
}

func TestSHA512(t *testing.T) {
	assert.Len(t, hashx.SHA512([]byte("abc")), 64)
	assert.Len(t, hashx.SHA512Hex([]byte("abc")), 128)
}

func TestHMACSHA256_KnownAnswer(t *testing.T) {
	// RFC 4231 test case 2
	const want = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	got := hashx.HMACSHA256Hex([]byte("Jefe"), []byte("what do ya want for nothing?"))
	assert.Equal(t, want, got)
	assert.Len(t, hashx.HMACSHA256([]byte("k"), []byte("m")), 32)
}

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	require.NoError(t, os.WriteFile(p, []byte("abc"), 0o600))
	got, err := hashx.FileSHA256(p)
	require.NoError(t, err)
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", got)
}

func TestFileSHA256_Missing(t *testing.T) {
	_, err := hashx.FileSHA256(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./hashx/`
Expected: FAIL — `undefined: hashx.SHA256Hex`.

- [ ] **Step 3: Write the implementation**

`hashx/hashx.go`:
```go
package hashx

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// SHA256 returns the SHA-256 digest of b.
func SHA256(b []byte) []byte { s := sha256.Sum256(b); return s[:] }

// SHA256Hex returns the lowercase-hex SHA-256 digest of b.
func SHA256Hex(b []byte) string { return hex.EncodeToString(SHA256(b)) }

// SHA256Base64 returns the unpadded standard-base64 SHA-256 digest of b.
func SHA256Base64(b []byte) string { return base64.RawStdEncoding.EncodeToString(SHA256(b)) }

// SHA512 returns the SHA-512 digest of b.
func SHA512(b []byte) []byte { s := sha512.Sum512(b); return s[:] }

// SHA512Hex returns the lowercase-hex SHA-512 digest of b.
func SHA512Hex(b []byte) string { return hex.EncodeToString(SHA512(b)) }

// HMACSHA256 returns the HMAC-SHA256 of msg under key.
func HMACSHA256(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

// HMACSHA256Hex returns the lowercase-hex HMAC-SHA256 of msg under key.
func HMACSHA256Hex(key, msg []byte) string { return hex.EncodeToString(HMACSHA256(key, msg)) }

// FileSHA256 streams the file at path and returns its lowercase-hex SHA-256 digest.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hashx: open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashx: read %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

`hashx/doc.go`:
```go
// Package hashx provides convenience digest helpers — SHA-256/512 in raw, hex, and
// base64 form, HMAC-SHA256, and a streaming file hash — removing per-call boilerplate
// for ETags, cache keys, content addressing, and dedup. It deliberately excludes the
// insecure MD5 and SHA-1 digests.
//
//	etag := hashx.SHA256Hex(body)
//	sum, err := hashx.FileSHA256("/path/to/upload")
package hashx
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./hashx/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add hashx/
git commit -m "feat(hashx): add SHA-256/512, HMAC, and streaming file digest helpers"
```

---

## Task 5: `redact` — keep secrets out of logs & JSON

**Files:**
- Create: `redact/redact.go`
- Create: `redact/doc.go`
- Test: `redact/redact_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces: `type Secret[T any]`; `func New[T any](v T) Secret[T]`; `(Secret[T]).Expose() T`; `(Secret[T]).String() string`; `(Secret[T]).GoString() string`; `(Secret[T]).MarshalJSON() ([]byte, error)`; `(Secret[T]).LogValue() slog.Value`; `func String(s string) string`; `func Map(m map[string]any, keys ...string) map[string]any`.

- [ ] **Step 1: Write the failing test**

`redact/redact_test.go`:
```go
package redact_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/redact"
)

func TestSecret_Expose(t *testing.T) {
	s := redact.New("super-secret")
	assert.Equal(t, "super-secret", s.Expose())
}

func TestSecret_StringMasks(t *testing.T) {
	s := redact.New("super-secret")
	assert.Equal(t, "REDACTED", s.String())
	assert.Equal(t, "REDACTED", fmt.Sprintf("%v", s))
	assert.Equal(t, "REDACTED", fmt.Sprintf("%s", s))
	assert.Equal(t, "REDACTED", fmt.Sprintf("%#v", s)) // GoString
}

func TestSecret_JSONMasks(t *testing.T) {
	type cfg struct {
		Name string                `json:"name"`
		Key  redact.Secret[string] `json:"key"`
	}
	out, err := json.Marshal(cfg{Name: "app", Key: redact.New("sk_live_123")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"app","key":"REDACTED"}`, string(out))
}

func TestSecret_LogValueMasks(t *testing.T) {
	s := redact.New([]byte("bytes-secret"))
	assert.Equal(t, "REDACTED", s.LogValue().String())
}

func TestString(t *testing.T) {
	assert.Equal(t, "sk_l***f8a2", redact.String("sk_live_abcdef8a2"[:7]+"f8a2")) // 11-char input
	assert.Equal(t, "REDACTED", redact.String("short"))     // too short to mask safely
	assert.Equal(t, "REDACTED", redact.String("12345678"))  // == 2*keep, still masked whole
}

func TestMap_ScrubsWithoutMutating(t *testing.T) {
	in := map[string]any{"user": "ada", "password": "hunter2", "token": "t_abc"}
	out := redact.Map(in, "password", "token", "absent")
	assert.Equal(t, "REDACTED", out["password"])
	assert.Equal(t, "REDACTED", out["token"])
	assert.Equal(t, "ada", out["user"])
	// original is untouched
	assert.Equal(t, "hunter2", in["password"])
	_, hasAbsent := out["absent"]
	assert.False(t, hasAbsent)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./redact/`
Expected: FAIL — `undefined: redact.New`.

- [ ] **Step 3: Write the implementation**

`redact/redact.go`:
```go
package redact

import (
	"encoding/json"
	"log/slog"
)

const placeholder = "REDACTED"

// Secret wraps a value so it renders as "REDACTED" through fmt, encoding/json, and
// log/slog, and is revealed only via Expose.
type Secret[T any] struct {
	v T
}

// New wraps v in a Secret.
func New[T any](v T) Secret[T] { return Secret[T]{v: v} }

// Expose returns the wrapped value. This is the only way to read it.
func (s Secret[T]) Expose() T { return s.v }

// String implements fmt.Stringer.
func (s Secret[T]) String() string { return placeholder }

// GoString implements fmt.GoStringer (the %#v verb).
func (s Secret[T]) GoString() string { return placeholder }

// MarshalJSON implements json.Marshaler.
func (s Secret[T]) MarshalJSON() ([]byte, error) { return json.Marshal(placeholder) }

// LogValue implements slog.LogValuer.
func (s Secret[T]) LogValue() slog.Value { return slog.StringValue(placeholder) }

// String returns a partially masked copy of s, keeping a short prefix and suffix for
// correlation (e.g. "sk_l***f8a2"). Strings of 8 characters or fewer are fully masked.
func String(s string) string {
	const keep = 4
	if len(s) <= keep*2 {
		return placeholder
	}
	return s[:keep] + "***" + s[len(s)-keep:]
}

// Map returns a shallow copy of m with the named keys replaced by "REDACTED". The
// input map is not modified; keys not present are ignored.
func Map(m map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	for _, k := range keys {
		if _, ok := out[k]; ok {
			out[k] = placeholder
		}
	}
	return out
}
```

`redact/doc.go`:
```go
// Package redact keeps secrets out of logs, error strings, and JSON. The Secret[T]
// wrapper renders as "REDACTED" through fmt, encoding/json, and log/slog, and reveals
// its value only via Expose, so logging a whole config or request cannot leak a wrapped
// field. The free functions String and Map scrub data you do not control.
//
//	type Config struct {
//		StripeKey redact.Secret[string]
//	}
//	slog.Info("config", "cfg", cfg)            // StripeKey logs as REDACTED
//	client := stripe.New(cfg.StripeKey.Expose())
//
//	safe := redact.Map(payload, "password", "token")
//	slog.Info("webhook", "body", safe)
//
// redact does not fetch from a vault; it is purely a wrapper plus scrub helpers.
package redact
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./redact/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add redact/
git commit -m "feat(redact): add Secret[T] wrapper and scrub helpers"
```

---

## Task 6: `kdf` — key derivation (and promote x/crypto to a direct dep)

**Files:**
- Create: `kdf/kdf.go`
- Create: `kdf/errors.go`
- Create: `kdf/doc.go`
- Test: `kdf/kdf_test.go`
- Modify: `go.mod` (via `go mod tidy` — `golang.org/x/crypto` moves from indirect to direct)

**Interfaces:**
- Consumes: `golang.org/x/crypto/argon2`, stdlib `crypto/hkdf`.
- Produces: `type Params struct { Time, Memory, KeyLen uint32; Threads uint8 }`; `func DefaultParams() Params`; `(Params).Validate() error`; `func DeriveKey(passphrase, salt []byte, p Params) ([]byte, error)`; `func HKDF(secret, salt, info []byte, length int) ([]byte, error)`; `var ErrInvalidParams`.

- [ ] **Step 1: Write the failing test**

`kdf/kdf_test.go`:
```go
package kdf_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/kdf"
)

func TestHKDF_RFC5869Case1(t *testing.T) {
	ikm := make([]byte, 22)
	for i := range ikm {
		ikm[i] = 0x0b
	}
	salt := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	info := []byte{0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9}
	const want = "3cb25f25faacd57a90434f64d0362f2a" +
		"2d2d0a90cf1a5a4c5db02d56ecc4c5bf" +
		"34007208d5b887185865"

	got, err := kdf.HKDF(ikm, salt, info, 42)
	require.NoError(t, err)
	assert.Equal(t, want, hex.EncodeToString(got))
}

func TestHKDF_DomainSeparation(t *testing.T) {
	master := []byte("master-secret-material")
	salt := []byte("app-v1")
	a, err := kdf.HKDF(master, salt, []byte("cookie"), 32)
	require.NoError(t, err)
	b, err := kdf.HKDF(master, salt, []byte("token"), 32)
	require.NoError(t, err)
	assert.Len(t, a, 32)
	assert.NotEqual(t, a, b) // different info → unrelated keys
}

func TestDeriveKey_Deterministic(t *testing.T) {
	p := kdf.Params{Time: 1, Memory: 8 * 1024, Threads: 1, KeyLen: 32}
	salt := []byte("0123456789abcdef")
	a, err := kdf.DeriveKey([]byte("passphrase"), salt, p)
	require.NoError(t, err)
	b, err := kdf.DeriveKey([]byte("passphrase"), salt, p)
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.Len(t, a, 32)

	c, err := kdf.DeriveKey([]byte("passphrase"), []byte("different-salt16"), p)
	require.NoError(t, err)
	assert.NotEqual(t, a, c)
}

func TestParams_Validate(t *testing.T) {
	require.NoError(t, kdf.DefaultParams().Validate())
	require.ErrorIs(t, (kdf.Params{}).Validate(), kdf.ErrInvalidParams)
	require.ErrorIs(t, kdf.Params{Time: 1, Memory: 0, Threads: 1, KeyLen: 32}.Validate(), kdf.ErrInvalidParams)
}

func TestDeriveKey_RejectsBadParams(t *testing.T) {
	_, err := kdf.DeriveKey([]byte("p"), []byte("s"), kdf.Params{})
	require.ErrorIs(t, err, kdf.ErrInvalidParams)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./kdf/`
Expected: FAIL — `undefined: kdf.HKDF`.

- [ ] **Step 3: Write the implementation**

`kdf/errors.go`:
```go
package kdf

import "errors"

// ErrInvalidParams is returned when Params has a zero field.
var ErrInvalidParams = errors.New("kdf: invalid params")
```

`kdf/kdf.go`:
```go
package kdf

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Params holds Argon2id cost parameters, shared with the password package.
type Params struct {
	Time    uint32 // iterations
	Memory  uint32 // memory in KiB
	KeyLen  uint32 // output length in bytes
	Threads uint8  // parallelism
}

// DefaultParams returns sane Argon2id parameters (t=3, m=64MiB, p=4, 32-byte key).
func DefaultParams() Params {
	return Params{Time: 3, Memory: 64 * 1024, KeyLen: 32, Threads: 4}
}

// Validate reports whether every Params field is non-zero.
func (p Params) Validate() error {
	if p.Time == 0 || p.Memory == 0 || p.Threads == 0 || p.KeyLen == 0 {
		return fmt.Errorf("%w: all fields must be > 0", ErrInvalidParams)
	}
	return nil
}

// DeriveKey turns a low-entropy passphrase into KeyLen key bytes via Argon2id.
func DeriveKey(passphrase, salt []byte, p Params) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return argon2.IDKey(passphrase, salt, p.Time, p.Memory, p.Threads, p.KeyLen), nil
}

// HKDF derives length bytes from a high-entropy secret using HKDF-SHA256. The info
// argument domain-separates outputs, so one master secret yields cryptographically
// unrelated keys per purpose.
func HKDF(secret, salt, info []byte, length int) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, secret, salt, string(info), length)
	if err != nil {
		return nil, fmt.Errorf("kdf: hkdf: %w", err)
	}
	return key, nil
}
```

`kdf/doc.go`:
```go
// Package kdf derives key material. It has two jobs: HKDF turns one high-entropy master
// secret into many purpose-scoped, cryptographically unrelated keys (the info argument
// domain-separates them); DeriveKey turns a low-entropy user passphrase into key bytes
// via Argon2id.
//
//	cookieKey, _ := kdf.HKDF(master, salt, []byte("cookie-encryption"), 32)
//	tokenKey, _  := kdf.HKDF(master, salt, []byte("email-token-hmac"), 32)
//
//	key, _ := kdf.DeriveKey([]byte(passphrase), salt, kdf.DefaultParams())
//
// It is distinct from password (which verifies a login and never exposes the key) and
// keyset (which stores and rotates keys). Params is shared with package password.
package kdf
```

- [ ] **Step 4: Run test, then tidy the module**

Run: `go test -race -cover ./kdf/`
Expected: PASS.

Then promote `x/crypto` from indirect to direct now that it is imported by first-party code:

Run: `go mod tidy`
Expected: `golang.org/x/crypto` moves into the direct `require` block in `go.mod` (the `// indirect` comment is removed). No test breakage.

- [ ] **Step 5: Commit**

```bash
git add kdf/ go.mod go.sum
git commit -m "feat(kdf): add HKDF + Argon2id key derivation; promote x/crypto to direct dep"
```

---

## Task 7: `keyset` — in-memory versioned keyring

**Files:**
- Create: `keyset/keyset.go`
- Create: `keyset/options.go`
- Create: `keyset/errors.go`
- Create: `keyset/doc.go`
- Test: `keyset/keyset_test.go`

**Interfaces:**
- Consumes: stdlib only (`encoding/base64`, `iter`, `sort`, `strconv`, `strings`, `errors`). Note: the spec listed `subtlex` as a dep, but `keyset` performs no secret comparison, so it stays stdlib-only — record this in the commit body.
- Produces: `type Keyset struct{…}`; `type Option func(*config)`; `func New(opts ...Option) (*Keyset, error)`; `func WithPrimary(version int, key []byte) Option`; `func WithRetired(version int, key []byte) Option`; `func WithBase64Keys(s string) Option`; `(*Keyset).Primary() (int, []byte)`; `(*Keyset).ByVersion(v int) ([]byte, bool)`; `(*Keyset).All() iter.Seq2[int, []byte]`; `var ErrNoPrimary`, `var ErrBadKeyMaterial`.

- [ ] **Step 1: Write the failing test**

`keyset/keyset_test.go`:
```go
package keyset_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/keyset"
)

// base64.StdEncoding of 32 bytes of 0x01 and 0x02 respectively.
const (
	key1B64 = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	key2B64 = "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI="
)

func TestNew_PrimaryAndRetired(t *testing.T) {
	ks, err := keyset.New(
		keyset.WithPrimary(2, []byte("key-two")),
		keyset.WithRetired(1, []byte("key-one")),
	)
	require.NoError(t, err)

	ver, key := ks.Primary()
	assert.Equal(t, 2, ver)
	assert.Equal(t, []byte("key-two"), key)

	k1, ok := ks.ByVersion(1)
	assert.True(t, ok)
	assert.Equal(t, []byte("key-one"), k1)

	_, ok = ks.ByVersion(99)
	assert.False(t, ok)
}

func TestNew_NoPrimary(t *testing.T) {
	_, err := keyset.New(keyset.WithRetired(1, []byte("only-retired")))
	require.ErrorIs(t, err, keyset.ErrNoPrimary)
}

func TestWithBase64Keys(t *testing.T) {
	ks, err := keyset.New(keyset.WithBase64Keys("2:" + key2B64 + ",1:" + key1B64))
	require.NoError(t, err)

	ver, key := ks.Primary()
	assert.Equal(t, 2, ver) // highest version is primary
	assert.Len(t, key, 32)

	_, ok := ks.ByVersion(1)
	assert.True(t, ok)
}

func TestWithBase64Keys_Bad(t *testing.T) {
	_, err := keyset.New(keyset.WithBase64Keys("1:not-base64!!!"))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)

	_, err = keyset.New(keyset.WithBase64Keys("missing-colon"))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)

	_, err = keyset.New(keyset.WithBase64Keys(""))
	require.ErrorIs(t, err, keyset.ErrBadKeyMaterial)
}

func TestAll_DescendingOrder(t *testing.T) {
	ks, err := keyset.New(
		keyset.WithPrimary(3, []byte("c")),
		keyset.WithRetired(1, []byte("a")),
		keyset.WithRetired(2, []byte("b")),
	)
	require.NoError(t, err)

	var versions []int
	for v := range ks.All() {
		versions = append(versions, v)
	}
	assert.Equal(t, []int{3, 2, 1}, versions)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./keyset/`
Expected: FAIL — `undefined: keyset.New`.

- [ ] **Step 3: Write the implementation**

`keyset/errors.go`:
```go
package keyset

import "errors"

// ErrNoPrimary is returned by New when no primary key was configured.
var ErrNoPrimary = errors.New("keyset: no primary key configured")

// ErrBadKeyMaterial is returned when supplied key material is malformed.
var ErrBadKeyMaterial = errors.New("keyset: invalid key material")
```

`keyset/options.go`:
```go
package keyset

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// config accumulates keys and option errors before New validates them.
type config struct {
	keys       map[int][]byte
	errs       []error
	primary    int
	hasPrimary bool
}

// Option configures New. Invalid values accumulate and are returned by New.
type Option func(*config)

// WithPrimary registers key as the primary (encrypting/signing) key at version.
func WithPrimary(version int, key []byte) Option {
	return func(c *config) {
		if version < 0 || len(key) == 0 {
			c.errs = append(c.errs, fmt.Errorf("%w: primary version %d", ErrBadKeyMaterial, version))
			return
		}
		c.keys[version] = key
		c.primary = version
		c.hasPrimary = true
	}
}

// WithRetired registers a retired (decrypt/verify-only) key at version.
func WithRetired(version int, key []byte) Option {
	return func(c *config) {
		if version < 0 || len(key) == 0 {
			c.errs = append(c.errs, fmt.Errorf("%w: retired version %d", ErrBadKeyMaterial, version))
			return
		}
		c.keys[version] = key
	}
}

// WithBase64Keys parses comma-separated "version:base64" pairs (typically one env var).
// The highest version becomes primary; the rest are retired.
func WithBase64Keys(s string) Option {
	return func(c *config) {
		if strings.TrimSpace(s) == "" {
			c.errs = append(c.errs, fmt.Errorf("%w: empty key material", ErrBadKeyMaterial))
			return
		}
		for _, pair := range strings.Split(s, ",") {
			verStr, b64, ok := strings.Cut(strings.TrimSpace(pair), ":")
			if !ok {
				c.errs = append(c.errs, fmt.Errorf("%w: %q missing version", ErrBadKeyMaterial, pair))
				return
			}
			v, err := strconv.Atoi(strings.TrimSpace(verStr))
			if err != nil || v < 0 {
				c.errs = append(c.errs, fmt.Errorf("%w: bad version %q", ErrBadKeyMaterial, verStr))
				return
			}
			key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
			if err != nil || len(key) == 0 {
				c.errs = append(c.errs, fmt.Errorf("%w: bad base64 for version %d", ErrBadKeyMaterial, v))
				return
			}
			c.keys[v] = key
			if !c.hasPrimary || v > c.primary {
				c.primary = v
				c.hasPrimary = true
			}
		}
	}
}
```

`keyset/keyset.go`:
```go
package keyset

import (
	"errors"
	"iter"
	"sort"
)

// Keyset is an in-memory versioned keyring: a primary key for new operations plus any
// number of retired keys for decrypting/verifying older material during rotation.
type Keyset struct {
	keys    map[int][]byte
	primary int
}

// New builds a Keyset from the given options. It returns ErrNoPrimary if no primary was
// set and joins any option errors (ErrBadKeyMaterial).
func New(opts ...Option) (*Keyset, error) {
	c := &config{keys: make(map[int][]byte)}
	for _, o := range opts {
		o(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if !c.hasPrimary {
		return nil, ErrNoPrimary
	}
	return &Keyset{keys: c.keys, primary: c.primary}, nil
}

// Primary returns the current primary version and key.
func (k *Keyset) Primary() (int, []byte) { return k.primary, k.keys[k.primary] }

// ByVersion returns the key for v, and whether it exists.
func (k *Keyset) ByVersion(v int) ([]byte, bool) {
	key, ok := k.keys[v]
	return key, ok
}

// All iterates every (version, key) pair in descending version order.
func (k *Keyset) All() iter.Seq2[int, []byte] {
	return func(yield func(int, []byte) bool) {
		versions := make([]int, 0, len(k.keys))
		for v := range k.keys {
			versions = append(versions, v)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(versions)))
		for _, v := range versions {
			if !yield(v, k.keys[v]) {
				return
			}
		}
	}
}
```

`keyset/doc.go`:
```go
// Package keyset is an in-memory versioned keyring backing key rotation for the sign,
// secret, and token packages. It holds one primary key (used for new operations) plus
// any number of retired keys (used to decrypt or verify older material), loaded from
// base64 environment material or set explicitly.
//
//	ks, _ := keyset.New(keyset.WithBase64Keys(os.Getenv("FORGE_SECRET_KEYS")))
//	// FORGE_SECRET_KEYS = "2:<base64 new>,1:<base64 old>"
//	box, _ := secret.FromKeyset(ks)
//
// It is not a cloud KMS client; fetching secrets from a vault belongs to secretsource.
package keyset
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./keyset/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add keyset/
git commit -m "feat(keyset): add in-memory versioned keyring with base64 env loading

keyset performs no secret comparison, so it stays stdlib-only (no subtlex dep,
deviating from the spec's dependency sketch)."
```

---

## Task 8: `sign` — HMAC sign/verify

**Files:**
- Create: `sign/sign.go`
- Create: `sign/options.go`
- Create: `sign/errors.go`
- Create: `sign/doc.go`
- Test: `sign/sign_test.go`

**Interfaces:**
- Consumes: `keyset` (`*keyset.Keyset`, `Primary`, `ByVersion`), `subtlex` (`BytesEqual`), stdlib `crypto/hmac`/`crypto/sha256`/`hash`.
- Produces: `type Signer struct{…}`; `type Option func(*config)`; `func New(key []byte, opts ...Option) (*Signer, error)`; `func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Signer, error)`; `func WithHash(h func() hash.Hash) Option`; `(*Signer).Sign(msg []byte) []byte`; `(*Signer).Verify(msg, mac []byte) bool`; `(*Signer).SignString(msg string) string`; `(*Signer).VerifyString(msg, signed string) bool`; `var ErrInvalidKey`, `var ErrBadSignature`.

- [ ] **Step 1: Write the failing test**

`sign/sign_test.go`:
```go
package sign_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/sign"
)

func TestSignVerify_RawKey(t *testing.T) {
	s, err := sign.New([]byte("0123456789abcdef"))
	require.NoError(t, err)

	mac := s.Sign([]byte("hello"))
	assert.NotEmpty(t, mac)
	assert.True(t, s.Verify([]byte("hello"), mac))
	assert.False(t, s.Verify([]byte("hellp"), mac)) // tampered message
	assert.False(t, s.Verify([]byte("hello"), append(mac, 0x00)))
}

func TestNew_EmptyKey(t *testing.T) {
	_, err := sign.New(nil)
	require.ErrorIs(t, err, sign.ErrInvalidKey)
}

func TestSignVerifyString(t *testing.T) {
	s, err := sign.New([]byte("0123456789abcdef"))
	require.NoError(t, err)

	signed := s.SignString("payload")
	assert.Contains(t, signed, ".") // "version.mac"
	assert.True(t, s.VerifyString("payload", signed))
	assert.False(t, s.VerifyString("payload2", signed))
	assert.False(t, s.VerifyString("payload", "garbage"))
	assert.False(t, s.VerifyString("payload", "0.bad$$base64"))
}

func TestVerifyString_Rotation(t *testing.T) {
	// Sign under a keyset whose primary is version 1.
	ksOld, err := keyset.New(keyset.WithPrimary(1, []byte("key-v1-secret-bytes")))
	require.NoError(t, err)
	signerOld, err := sign.FromKeyset(ksOld)
	require.NoError(t, err)
	signed := signerOld.SignString("invite-payload")

	// Rotate: new primary v2, v1 retired. The new signer still verifies v1 material.
	ksNew, err := keyset.New(
		keyset.WithPrimary(2, []byte("key-v2-secret-bytes")),
		keyset.WithRetired(1, []byte("key-v1-secret-bytes")),
	)
	require.NoError(t, err)
	signerNew, err := sign.FromKeyset(ksNew)
	require.NoError(t, err)

	assert.True(t, signerNew.VerifyString("invite-payload", signed))
	assert.False(t, signerNew.VerifyString("tampered", signed))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./sign/`
Expected: FAIL — `undefined: sign.New`.

- [ ] **Step 3: Write the implementation**

`sign/errors.go`:
```go
package sign

import "errors"

// ErrInvalidKey is returned when a signer is built with no usable key.
var ErrInvalidKey = errors.New("sign: invalid key")

// ErrBadSignature is exported for callers that surface a typed verification failure; the
// Verify/VerifyString methods themselves return false rather than an error.
var ErrBadSignature = errors.New("sign: signature mismatch")
```

`sign/options.go`:
```go
package sign

import (
	"crypto/sha256"
	"fmt"
	"hash"
)

// config holds resolved signer settings before construction.
type config struct {
	hash func() hash.Hash
	errs []error
}

// Option configures New/FromKeyset. Invalid values accumulate and are returned.
type Option func(*config)

func newConfig(opts ...Option) *config {
	c := &config{hash: sha256.New}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithHash sets the HMAC hash constructor (default sha256.New). A nil value is rejected.
func WithHash(h func() hash.Hash) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: nil hash", ErrInvalidKey))
			return
		}
		c.hash = h
	}
}
```

`sign/sign.go`:
```go
package sign

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"hash"
	"strconv"
	"strings"

	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/subtlex"
)

// Signer produces and verifies HMAC tags. Sign/Verify operate on a single key; the
// String forms carry the key version so a keyset-backed signer can verify material
// produced under retired keys (transparent rotation).
type Signer struct {
	ks   *keyset.Keyset // nil for a raw single-key signer
	key  []byte
	hash func() hash.Hash
	ver  int
}

// New builds a single-key signer. An empty key returns ErrInvalidKey.
func New(key []byte, opts ...Option) (*Signer, error) {
	if len(key) == 0 {
		return nil, ErrInvalidKey
	}
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return &Signer{key: key, hash: c.hash}, nil
}

// FromKeyset builds a rotation-aware signer: it signs with the keyset's primary and
// verifies String material against any version (including retired keys).
func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Signer, error) {
	if ks == nil {
		return nil, ErrInvalidKey
	}
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	ver, key := ks.Primary()
	return &Signer{ks: ks, key: key, ver: ver, hash: c.hash}, nil
}

func (s *Signer) mac(key, msg []byte) []byte {
	m := hmac.New(s.hash, key)
	m.Write(msg)
	return m.Sum(nil)
}

// Sign returns the raw MAC of msg under the primary key.
func (s *Signer) Sign(msg []byte) []byte { return s.mac(s.key, msg) }

// Verify reports whether mac is a valid MAC for msg, in constant time.
func (s *Signer) Verify(msg, mac []byte) bool {
	return subtlex.BytesEqual(s.mac(s.key, msg), mac)
}

// SignString returns "<version>.<base64url-mac>" for msg.
func (s *Signer) SignString(msg string) string {
	mac := s.mac(s.key, []byte(msg))
	return strconv.Itoa(s.ver) + "." + base64.RawURLEncoding.EncodeToString(mac)
}

// VerifyString verifies a "<version>.<base64url-mac>" string against msg, resolving the
// key by version when this signer is keyset-backed. Returns false on any parse failure.
func (s *Signer) VerifyString(msg, signed string) bool {
	verStr, macStr, ok := strings.Cut(signed, ".")
	if !ok {
		return false
	}
	ver, err := strconv.Atoi(verStr)
	if err != nil {
		return false
	}
	mac, err := base64.RawURLEncoding.DecodeString(macStr)
	if err != nil {
		return false
	}
	key := s.key
	switch {
	case s.ks != nil:
		k, found := s.ks.ByVersion(ver)
		if !found {
			return false
		}
		key = k
	case ver != s.ver:
		return false
	}
	return subtlex.BytesEqual(s.mac(key, []byte(msg)), mac)
}
```

`sign/doc.go`:
```go
// Package sign produces and verifies HMAC tags with constant-time verification, for
// opaque values that must not be tampered with: unsubscribe links, signed download URLs,
// integrity checks. It is lower-level than token.
//
//	s, _ := sign.New(key)
//	tag := s.SignString("user@example.com") // "0.<base64url-mac>"
//	ok := s.VerifyString("user@example.com", tag)
//
// Sign/Verify are the raw single-key primitive; SignString/VerifyString carry the key
// version, so a signer built with FromKeyset verifies tags produced under retired keys
// during rotation. Verification always runs in constant time via subtlex.
package sign
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./sign/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sign/
git commit -m "feat(sign): add HMAC signer with constant-time verify and keyset rotation"
```

---

## Task 9: `password` — Argon2id hashing with bcrypt fallback

**Files:**
- Create: `password/password.go`
- Create: `password/options.go`
- Create: `password/errors.go`
- Create: `password/doc.go`
- Test: `password/password_test.go`

**Interfaces:**
- Consumes: `kdf` (`Params`, `DefaultParams`), `subtlex` (`BytesEqual`), `randx` (`Bytes`), `golang.org/x/crypto/argon2`, `golang.org/x/crypto/bcrypt`.
- Produces: `type Algorithm int` with `const ( Argon2id Algorithm = iota; Bcrypt )`; `func Hash(password string, opts ...Option) (string, error)`; `func Verify(password, encoded string) (ok bool, needsRehash bool, err error)`; `func WithAlgorithm(a Algorithm) Option`; `func WithArgon2Params(p kdf.Params) Option`; `func WithBcryptCost(cost int) Option`; `var ErrMismatch`, `var ErrInvalidHash`.

- [ ] **Step 1: Write the failing test**

`password/password_test.go`:
```go
package password_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/kdf"
	"github.com/dmitrymomot/forge/password"
)

// Light params keep the memory-hard hash fast in tests.
func lightParams() kdf.Params {
	return kdf.Params{Time: 1, Memory: 8 * 1024, Threads: 1, KeyLen: 32}
}

func TestHashVerify_Argon2id(t *testing.T) {
	enc, err := password.Hash("hunter2", password.WithArgon2Params(lightParams()))
	require.NoError(t, err)
	assert.Contains(t, enc, "$argon2id$")

	ok, _, err := password.Verify("hunter2", enc)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, _, err = password.Verify("wrong", enc)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestVerify_NeedsRehash(t *testing.T) {
	// Hashed with weaker-than-default params → should request a rehash.
	enc, err := password.Hash("pw", password.WithArgon2Params(lightParams()))
	require.NoError(t, err)
	ok, needsRehash, err := password.Verify("pw", enc)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, needsRehash)
}

func TestVerify_DefaultParamsNoRehash(t *testing.T) {
	enc, err := password.Hash("pw") // default params
	require.NoError(t, err)
	ok, needsRehash, err := password.Verify("pw", enc)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, needsRehash)
}

func TestHashVerify_Bcrypt(t *testing.T) {
	enc, err := password.Hash("hunter2",
		password.WithAlgorithm(password.Bcrypt),
		password.WithBcryptCost(4)) // minimum cost for fast tests
	require.NoError(t, err)
	assert.Contains(t, enc, "$2")

	ok, needsRehash, err := password.Verify("hunter2", enc)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, needsRehash) // bcrypt stored, argon2id is the target → migrate on login

	ok, _, err = password.Verify("wrong", enc)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestVerify_Malformed(t *testing.T) {
	_, _, err := password.Verify("pw", "not-a-hash")
	require.ErrorIs(t, err, password.ErrInvalidHash)

	_, _, err = password.Verify("pw", "$argon2id$v=19$bad")
	require.ErrorIs(t, err, password.ErrInvalidHash)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./password/`
Expected: FAIL — `undefined: password.Hash`.

- [ ] **Step 3: Write the implementation**

`password/errors.go`:
```go
package password

import "errors"

// ErrMismatch is exported for callers that prefer a typed mismatch; Verify itself
// reports a wrong password via its ok return value, not this error.
var ErrMismatch = errors.New("password: hash mismatch")

// ErrInvalidHash is returned when the encoded hash cannot be parsed.
var ErrInvalidHash = errors.New("password: malformed encoded hash")
```

`password/options.go`:
```go
package password

import (
	"golang.org/x/crypto/bcrypt"

	"github.com/dmitrymomot/forge/kdf"
)

// Algorithm selects the hashing scheme.
type Algorithm int

const (
	// Argon2id is the default, recommended algorithm.
	Argon2id Algorithm = iota
	// Bcrypt is provided as a fallback / migration source.
	Bcrypt
)

type config struct {
	argon kdf.Params
	algo  Algorithm
	bcost int
}

// Option configures Hash.
type Option func(*config)

func newConfig(opts ...Option) *config {
	c := &config{algo: Argon2id, argon: kdf.DefaultParams(), bcost: bcrypt.DefaultCost}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithAlgorithm selects the hashing algorithm (default Argon2id).
func WithAlgorithm(a Algorithm) Option { return func(c *config) { c.algo = a } }

// WithArgon2Params overrides the Argon2id cost parameters.
func WithArgon2Params(p kdf.Params) Option { return func(c *config) { c.argon = p } }

// WithBcryptCost overrides the bcrypt cost (used only with Algorithm Bcrypt).
func WithBcryptCost(cost int) Option { return func(c *config) { c.bcost = cost } }
```

`password/password.go`:
```go
package password

import (
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/dmitrymomot/forge/kdf"
	"github.com/dmitrymomot/forge/randx"
	"github.com/dmitrymomot/forge/subtlex"
)

const saltLen = 16

// Hash hashes password into a self-describing PHC string (Argon2id by default; bcrypt
// when Algorithm Bcrypt is selected).
func Hash(password string, opts ...Option) (string, error) {
	c := newConfig(opts...)
	if c.algo == Bcrypt {
		h, err := bcrypt.GenerateFromPassword([]byte(password), c.bcost)
		if err != nil {
			return "", fmt.Errorf("password: bcrypt: %w", err)
		}
		return string(h), nil
	}
	if err := c.argon.Validate(); err != nil {
		return "", err
	}
	salt := randx.Bytes(saltLen)
	key := argon2.IDKey([]byte(password), salt, c.argon.Time, c.argon.Memory, c.argon.Threads, c.argon.KeyLen)
	return encodeArgon(c.argon, salt, key), nil
}

func encodeArgon(p kdf.Params, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// Verify checks password against encoded, detecting the algorithm from the encoded
// prefix. ok reports whether the password matches; needsRehash is true when the stored
// parameters/algorithm differ from the current defaults (rehash on next login); err is
// non-nil only when encoded is malformed.
func Verify(password, encoded string) (ok bool, needsRehash bool, err error) {
	switch {
	case strings.HasPrefix(encoded, "$argon2id$"):
		return verifyArgon(password, encoded)
	case strings.HasPrefix(encoded, "$2a$"),
		strings.HasPrefix(encoded, "$2b$"),
		strings.HasPrefix(encoded, "$2y$"):
		e := bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password))
		switch {
		case e == nil:
			// Stored as bcrypt; the default target is Argon2id → migrate on login.
			return true, true, nil
		case errIsBcryptMismatch(e):
			return false, false, nil
		default:
			return false, false, fmt.Errorf("%w: %v", ErrInvalidHash, e)
		}
	default:
		return false, false, ErrInvalidHash
	}
}

func errIsBcryptMismatch(e error) bool {
	return e == bcrypt.ErrMismatchedHashAndPassword
}

func verifyArgon(password, encoded string) (bool, bool, error) {
	parts := strings.Split(encoded, "$") // ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 {
		return false, false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, false, ErrInvalidHash
	}
	var p kdf.Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false, false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, false, ErrInvalidHash
	}
	p.KeyLen = uint32(len(want))
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	if !subtlex.BytesEqual(got, want) {
		return false, false, nil
	}
	def := kdf.DefaultParams()
	needs := p.Time != def.Time || p.Memory != def.Memory || p.Threads != def.Threads || p.KeyLen != def.KeyLen
	return true, needs, nil
}
```

> Note: `bcrypt.ErrMismatchedHashAndPassword` is a sentinel error value; comparing with `==` (as `errIsBcryptMismatch` does) is correct and avoids an `errors.Is` import. If golangci-lint's `err113` flags the `==`, switch to `errors.Is(e, bcrypt.ErrMismatchedHashAndPassword)` and add the `errors` import.

`password/doc.go`:
```go
// Package password hashes and verifies user passwords. The default is Argon2id with a
// self-describing PHC string; bcrypt is available as a fallback and migration source.
//
//	enc, _ := password.Hash(plaintext)
//	ok, needsRehash, err := password.Verify(plaintext, enc)
//	if err == nil && ok && needsRehash {
//		newEnc, _ := password.Hash(plaintext) // upgrade stored hash transparently
//	}
//
// Verify detects the algorithm from the encoded prefix, compares in constant time, and
// reports needsRehash when the stored parameters or algorithm differ from the current
// defaults — bcrypt-stored hashes always request a rehash to Argon2id. A wrong password
// returns ok=false with a nil error; only a malformed encoding returns ErrInvalidHash.
// Argon2 parameters are shared with package kdf.
package password
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./password/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add password/
git commit -m "feat(password): add Argon2id hashing with bcrypt fallback and needsRehash"
```

---

## Task 10: `secret` — authenticated symmetric encryption

**Files:**
- Create: `secret/secret.go`
- Create: `secret/options.go`
- Create: `secret/errors.go`
- Create: `secret/doc.go`
- Test: `secret/secret_test.go`

**Interfaces:**
- Consumes: `keyset` (`*keyset.Keyset`, `Primary`, `ByVersion`), `randx` (`Bytes`), `golang.org/x/crypto/chacha20poly1305`, stdlib `crypto/aes`/`crypto/cipher`.
- Produces: `type Box struct{…}`; `type Option func(*config)`; `func New(key []byte, opts ...Option) (*Box, error)`; `func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Box, error)`; `func WithAAD(aad []byte) Option`; `func WithChaCha() Option`; `(*Box).Encrypt([]byte) ([]byte, error)`; `(*Box).Decrypt([]byte) ([]byte, error)`; `(*Box).EncryptString(string) (string, error)`; `(*Box).DecryptString(string) (string, error)`; `var ErrInvalidKeySize`, `var ErrDecryptFailed`.

- [ ] **Step 1: Write the failing test**

`secret/secret_test.go`:
```go
package secret_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/secret"
)

func key32(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func TestEncryptDecrypt_GCM(t *testing.T) {
	box, err := secret.New(key32(1))
	require.NoError(t, err)

	ct, err := box.Encrypt([]byte("4111 1111 1111 1111"))
	require.NoError(t, err)
	assert.NotContains(t, string(ct), "4111") // ciphertext hides the plaintext

	pt, err := box.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, "4111 1111 1111 1111", string(pt))
}

func TestEncryptDecryptString(t *testing.T) {
	box, err := secret.New(key32(7))
	require.NoError(t, err)
	enc, err := box.EncryptString("hello")
	require.NoError(t, err)
	dec, err := box.DecryptString(enc)
	require.NoError(t, err)
	assert.Equal(t, "hello", dec)
}

func TestNew_BadKeySize(t *testing.T) {
	_, err := secret.New([]byte("too-short"))
	require.ErrorIs(t, err, secret.ErrInvalidKeySize)
}

func TestDecrypt_Tampered(t *testing.T) {
	box, err := secret.New(key32(2))
	require.NoError(t, err)
	ct, err := box.Encrypt([]byte("data"))
	require.NoError(t, err)
	ct[len(ct)-1] ^= 0xff // flip a tag byte
	_, err = box.Decrypt(ct)
	require.ErrorIs(t, err, secret.ErrDecryptFailed)
}

func TestDecryptString_BadBase64(t *testing.T) {
	box, err := secret.New(key32(3))
	require.NoError(t, err)
	_, err = box.DecryptString("not*base64")
	require.ErrorIs(t, err, secret.ErrDecryptFailed)
}

func TestChaCha_RoundTrip(t *testing.T) {
	box, err := secret.New(key32(4), secret.WithChaCha())
	require.NoError(t, err)
	ct, err := box.Encrypt([]byte("xchacha"))
	require.NoError(t, err)
	pt, err := box.Decrypt(ct)
	require.NoError(t, err)
	assert.Equal(t, "xchacha", string(pt))
}

func TestAAD_MustMatch(t *testing.T) {
	enc, err := secret.New(key32(5), secret.WithAAD([]byte("ctx-A")))
	require.NoError(t, err)
	ct, err := enc.Encrypt([]byte("secret"))
	require.NoError(t, err)

	dec, err := secret.New(key32(5), secret.WithAAD([]byte("ctx-B")))
	require.NoError(t, err)
	_, err = dec.Decrypt(ct)
	require.ErrorIs(t, err, secret.ErrDecryptFailed) // different AAD fails authentication
}

func TestRotation_ViaKeyset(t *testing.T) {
	ksOld, err := keyset.New(keyset.WithPrimary(1, key32(1)))
	require.NoError(t, err)
	boxOld, err := secret.FromKeyset(ksOld)
	require.NoError(t, err)
	ct, err := boxOld.Encrypt([]byte("legacy"))
	require.NoError(t, err)

	ksNew, err := keyset.New(
		keyset.WithPrimary(2, key32(2)),
		keyset.WithRetired(1, key32(1)),
	)
	require.NoError(t, err)
	boxNew, err := secret.FromKeyset(ksNew)
	require.NoError(t, err)

	pt, err := boxNew.Decrypt(ct) // old ciphertext still decrypts under retired key
	require.NoError(t, err)
	assert.Equal(t, "legacy", string(pt))

	ct2, err := boxNew.Encrypt([]byte("fresh")) // new writes use primary v2
	require.NoError(t, err)
	pt2, err := boxNew.Decrypt(ct2)
	require.NoError(t, err)
	assert.Equal(t, "fresh", string(pt2))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./secret/`
Expected: FAIL — `undefined: secret.New`.

- [ ] **Step 3: Write the implementation**

`secret/errors.go`:
```go
package secret

import "errors"

// ErrInvalidKeySize is returned when a key is not the size the cipher requires (32 bytes).
var ErrInvalidKeySize = errors.New("secret: invalid key size")

// ErrDecryptFailed is returned for any decryption failure — wrong key, unknown version,
// tampered or truncated ciphertext, bad AAD — without revealing which.
var ErrDecryptFailed = errors.New("secret: decryption failed")
```

`secret/options.go`:
```go
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const keySize = 32

// aeadFor builds an AEAD from a key.
type aeadFor func(key []byte) (cipher.AEAD, error)

type config struct {
	newAEAD aeadFor
	aad     []byte
	errs    []error
}

// Option configures New/FromKeyset. Invalid values accumulate and are returned.
type Option func(*config)

func newConfig(opts ...Option) *config {
	c := &config{newAEAD: newGCM}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithAAD binds additional authenticated data to every Encrypt/Decrypt. The same AAD
// must be supplied to decrypt; a mismatch fails authentication.
func WithAAD(aad []byte) Option { return func(c *config) { c.aad = aad } }

// WithChaCha switches from the default AES-256-GCM to XChaCha20-Poly1305 (24-byte nonce).
func WithChaCha() Option { return func(c *config) { c.newAEAD = newChaCha } }

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, ErrInvalidKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKeySize, err)
	}
	return cipher.NewGCM(block)
}

func newChaCha(key []byte) (cipher.AEAD, error) {
	if len(key) != chacha20poly1305.KeySize {
		return nil, ErrInvalidKeySize
	}
	return chacha20poly1305.NewX(key)
}
```

`secret/secret.go`:
```go
package secret

import (
	"encoding/base64"
	"errors"

	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/randx"
)

// Box performs authenticated symmetric encryption. Output is
// version-byte || nonce || ciphertext+tag; Decrypt reads the version and resolves the
// key (single key, or via keyset including retired keys) before authenticating.
type Box struct {
	ks      *keyset.Keyset // nil for single-key
	key     []byte
	aad     []byte
	newAEAD aeadFor
	ver     int
}

// New builds a single-key Box (AES-256-GCM by default, requiring a 32-byte key).
func New(key []byte, opts ...Option) (*Box, error) {
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if _, err := c.newAEAD(key); err != nil {
		return nil, err
	}
	return &Box{key: key, aad: c.aad, newAEAD: c.newAEAD}, nil
}

// FromKeyset builds a rotation-aware Box: it encrypts under the keyset's primary and
// decrypts material produced under any version (including retired keys).
func FromKeyset(ks *keyset.Keyset, opts ...Option) (*Box, error) {
	c := newConfig(opts...)
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if ks == nil {
		return nil, ErrInvalidKeySize
	}
	ver, key := ks.Primary()
	if _, err := c.newAEAD(key); err != nil {
		return nil, err
	}
	return &Box{ks: ks, key: key, ver: ver, aad: c.aad, newAEAD: c.newAEAD}, nil
}

// Encrypt seals plaintext, returning version-byte || nonce || ciphertext+tag.
func (b *Box) Encrypt(plaintext []byte) ([]byte, error) {
	if b.ver < 0 || b.ver > 255 {
		return nil, ErrInvalidKeySize
	}
	aead, err := b.newAEAD(b.key)
	if err != nil {
		return nil, err
	}
	nonce := randx.Bytes(aead.NonceSize())
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, byte(b.ver))
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, b.aad), nil
}

// Decrypt opens a ciphertext produced by Encrypt.
func (b *Box) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 1 {
		return nil, ErrDecryptFailed
	}
	ver := int(ciphertext[0])
	key := b.key
	switch {
	case b.ks != nil:
		k, ok := b.ks.ByVersion(ver)
		if !ok {
			return nil, ErrDecryptFailed
		}
		key = k
	case ver != b.ver:
		return nil, ErrDecryptFailed
	}
	aead, err := b.newAEAD(key)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	ns := aead.NonceSize()
	if len(ciphertext) < 1+ns {
		return nil, ErrDecryptFailed
	}
	nonce := ciphertext[1 : 1+ns]
	pt, err := aead.Open(nil, nonce, ciphertext[1+ns:], b.aad)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return pt, nil
}

// EncryptString is Encrypt with unpadded base64url in/out.
func (b *Box) EncryptString(s string) (string, error) {
	ct, err := b.Encrypt([]byte(s))
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ct), nil
}

// DecryptString is Decrypt with unpadded base64url in/out.
func (b *Box) DecryptString(s string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", ErrDecryptFailed
	}
	pt, err := b.Decrypt(raw)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
```

`secret/doc.go`:
```go
// Package secret provides authenticated symmetric encryption (an AEAD "secret box") of
// bytes or strings, with versioned ciphertext for key rotation. The default is
// AES-256-GCM (stdlib); WithChaCha selects XChaCha20-Poly1305 for high-volume
// random-nonce use. It underpins cookie/session encryption, encrypted tokens, and field
// crypto.
//
//	box, _ := secret.New(key)            // 32-byte key
//	ct, _  := box.EncryptString(pii)
//	pt, _  := box.DecryptString(ct)
//
//	// rotation:
//	box, _ := secret.FromKeyset(ks)      // encrypt under primary, decrypt under any version
//
// Ciphertext is version-byte || nonce || ciphertext+tag. Any decryption failure returns
// ErrDecryptFailed without revealing the cause (no padding/key oracle).
package secret
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./secret/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add secret/
git commit -m "feat(secret): add AES-256-GCM/XChaCha AEAD box with keyset rotation"
```

---

## Task 11: `token` — opaque, signed, expiring, purpose-bound tokens

**Files:**
- Create: `token/token.go`
- Create: `token/options.go`
- Create: `token/errors.go`
- Create: `token/doc.go`
- Test: `token/token_test.go`

**Interfaces:**
- Consumes: `sign` (`New`, `FromKeyset`, `SignString`, `VerifyString`), `secret` (`*secret.Box`, `Encrypt`, `Decrypt`), `keyset` (`*keyset.Keyset`), `clock` (`Clock`, `System`, `Mock`), `randx` (`URLSafe`), stdlib `encoding/json`/`encoding/base64`/`time`.
- Produces: `type Codec[T any] struct{…}`; `type Option func(*config)`; `func New[T any](key []byte, opts ...Option) (*Codec[T], error)`; `func FromKeyset[T any](ks *keyset.Keyset, opts ...Option) (*Codec[T], error)`; `func WithTTL(d time.Duration) Option`; `func WithPurpose(p string) Option`; `func WithEncrypt(box *secret.Box) Option`; `func WithClock(c clock.Clock) Option`; `(*Codec[T]).Issue(payload T) (string, error)`; `(*Codec[T]).Parse(token string) (T, error)`; `var ErrExpired`, `var ErrBadSignature`, `var ErrMalformed`, `var ErrWrongPurpose`.

- [ ] **Step 1: Write the failing test**

`token/token_test.go`:
```go
package token_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/clock"
	"github.com/dmitrymomot/forge/secret"
	"github.com/dmitrymomot/forge/token"
)

type Reset struct {
	UserID string `json:"uid"`
}

func TestIssueParse_RoundTrip(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"), token.WithPurpose("pwreset"))
	require.NoError(t, err)

	tok, err := c.Issue(Reset{UserID: "u_123"})
	require.NoError(t, err)

	got, err := c.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "u_123", got.UserID)
}

func TestParse_Expired(t *testing.T) {
	m := clock.NewMock(time.Unix(1_000_000, 0))
	c, err := token.New[Reset]([]byte("0123456789abcdef"),
		token.WithTTL(15*time.Minute), token.WithClock(m))
	require.NoError(t, err)

	tok, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	m.Advance(16 * time.Minute)
	_, err = c.Parse(tok)
	require.ErrorIs(t, err, token.ErrExpired)
}

func TestParse_NotYetExpired(t *testing.T) {
	m := clock.NewMock(time.Unix(1_000_000, 0))
	c, err := token.New[Reset]([]byte("0123456789abcdef"),
		token.WithTTL(15*time.Minute), token.WithClock(m))
	require.NoError(t, err)
	tok, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	m.Advance(14 * time.Minute)
	got, err := c.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "u_1", got.UserID)
}

func TestParse_Tampered(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"))
	require.NoError(t, err)
	tok, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	// Flip the first character of the body segment.
	b := []byte(tok)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	_, err = c.Parse(string(b))
	require.ErrorIs(t, err, token.ErrBadSignature)
}

func TestParse_WrongPurpose(t *testing.T) {
	key := []byte("0123456789abcdef")
	issuer, err := token.New[Reset](key, token.WithPurpose("pwreset"))
	require.NoError(t, err)
	tok, err := issuer.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)

	other, err := token.New[Reset](key, token.WithPurpose("magiclink"))
	require.NoError(t, err)
	_, err = other.Parse(tok)
	require.ErrorIs(t, err, token.ErrWrongPurpose)
}

func TestParse_Malformed(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"))
	require.NoError(t, err)
	_, err = c.Parse("garbage-no-dots")
	require.Error(t, err)
}

func TestEncrypted_RoundTrip(t *testing.T) {
	box, err := secret.New([]byte("0123456789abcdef0123456789abcdef")) // 32 bytes
	require.NoError(t, err)
	c, err := token.New[Reset]([]byte("0123456789abcdef"), token.WithEncrypt(box))
	require.NoError(t, err)

	tok, err := c.Issue(Reset{UserID: "secret-uid"})
	require.NoError(t, err)
	assert.NotContains(t, tok, "secret-uid") // payload is encrypted, not just signed

	got, err := c.Parse(tok)
	require.NoError(t, err)
	assert.Equal(t, "secret-uid", got.UserID)
}

func TestUniqueTokensForSamePayload(t *testing.T) {
	c, err := token.New[Reset]([]byte("0123456789abcdef"))
	require.NoError(t, err)
	a, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)
	b, err := c.Issue(Reset{UserID: "u_1"})
	require.NoError(t, err)
	assert.NotEqual(t, a, b) // per-token nonce
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./token/`
Expected: FAIL — `undefined: token.New`.

- [ ] **Step 3: Write the implementation**

`token/errors.go`:
```go
package token

import "errors"

// ErrExpired is returned by Parse when the token's expiry has passed.
var ErrExpired = errors.New("token: expired")

// ErrBadSignature is returned by Parse when the signature or encryption fails to verify.
var ErrBadSignature = errors.New("token: signature mismatch")

// ErrMalformed is returned by Parse when the token cannot be decoded.
var ErrMalformed = errors.New("token: malformed")

// ErrWrongPurpose is returned by Parse when the token's purpose does not match the codec.
var ErrWrongPurpose = errors.New("token: wrong purpose")
```

`token/options.go`:
```go
package token

import (
	"errors"
	"time"

	"github.com/dmitrymomot/forge/clock"
	"github.com/dmitrymomot/forge/secret"
)

type config struct {
	box     *secret.Box
	clk     clock.Clock
	purpose string
	ttl     time.Duration
	errs    []error
}

// Option configures New/FromKeyset.
type Option func(*config)

func newConfig(opts ...Option) (*config, error) {
	c := &config{clk: clock.System()}
	for _, o := range opts {
		o(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return c, nil
}

// WithTTL sets the token lifetime. A zero TTL (default) means the token never expires.
func WithTTL(d time.Duration) Option { return func(c *config) { c.ttl = d } }

// WithPurpose binds tokens to a named flow; Parse rejects a mismatched purpose.
func WithPurpose(p string) Option { return func(c *config) { c.purpose = p } }

// WithEncrypt encrypts the payload (not just signs it) using the given secret.Box.
func WithEncrypt(box *secret.Box) Option { return func(c *config) { c.box = box } }

// WithClock sets the time source (default clock.System()). A nil clock is rejected.
func WithClock(ck clock.Clock) Option {
	return func(c *config) {
		if ck == nil {
			c.errs = append(c.errs, errors.New("token: nil clock"))
			return
		}
		c.clk = ck
	}
}
```

`token/token.go`:
```go
package token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/clock"
	"github.com/dmitrymomot/forge/keyset"
	"github.com/dmitrymomot/forge/randx"
	"github.com/dmitrymomot/forge/secret"
	"github.com/dmitrymomot/forge/sign"
)

// envelope is the JSON-serialized token body.
type envelope[T any] struct {
	Purpose string `json:"prp,omitempty"`
	Nonce   string `json:"nce"`
	Payload T      `json:"pld"`
	Exp     int64  `json:"exp,omitempty"`
}

// Codec issues and parses opaque, signed, optionally-encrypted, expiring tokens carrying
// a payload of type T. It is deliberately not JWT.
type Codec[T any] struct {
	signer  *sign.Signer
	box     *secret.Box
	clk     clock.Clock
	purpose string
	ttl     time.Duration
}

// New builds a single-key Codec.
func New[T any](key []byte, opts ...Option) (*Codec[T], error) {
	signer, err := sign.New(key)
	if err != nil {
		return nil, err
	}
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	return &Codec[T]{signer: signer, box: c.box, clk: c.clk, purpose: c.purpose, ttl: c.ttl}, nil
}

// FromKeyset builds a rotation-aware Codec (signs under primary, verifies any version).
func FromKeyset[T any](ks *keyset.Keyset, opts ...Option) (*Codec[T], error) {
	signer, err := sign.FromKeyset(ks)
	if err != nil {
		return nil, err
	}
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	return &Codec[T]{signer: signer, box: c.box, clk: c.clk, purpose: c.purpose, ttl: c.ttl}, nil
}

// Issue marshals payload into a signed (optionally encrypted) url-safe token string.
func (c *Codec[T]) Issue(payload T) (string, error) {
	env := envelope[T]{Purpose: c.purpose, Nonce: randx.URLSafe(8), Payload: payload}
	if c.ttl > 0 {
		env.Exp = c.clk.Now().Add(c.ttl).Unix()
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if c.box != nil {
		enc, err := c.box.Encrypt(raw)
		if err != nil {
			return "", err
		}
		raw = enc
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + c.signer.SignString(body), nil // SignString returns "ver.mac"
}

// Parse verifies, decrypts (if applicable), and decodes a token back into a payload.
func (c *Codec[T]) Parse(token string) (T, error) {
	var zero T
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" {
		return zero, ErrMalformed
	}
	if !c.signer.VerifyString(body, sig) {
		return zero, ErrBadSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return zero, ErrMalformed
	}
	if c.box != nil {
		dec, err := c.box.Decrypt(raw)
		if err != nil {
			return zero, ErrBadSignature // authenticated-encryption failure ~ tamper
		}
		raw = dec
	}
	var env envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return zero, ErrMalformed
	}
	if env.Exp != 0 && c.clk.Now().Unix() > env.Exp {
		return zero, ErrExpired
	}
	if env.Purpose != c.purpose {
		return zero, ErrWrongPurpose
	}
	return env.Payload, nil
}
```

> Note on the wire format: `SignString` returns `"<ver>.<mac>"`, so a token is
> `body.ver.mac` (two dots). `Parse` splits on the *first* dot via `strings.Cut`: the
> left part is `body` (base64url, contains no dot), the right part is `ver.mac` which is
> exactly what `VerifyString` expects. No three-way split is needed.

`token/doc.go`:
```go
// Package token issues and parses opaque, signed, optionally-encrypted, expiring tokens
// that carry a small typed payload — for email-verify, password-reset, magic-link, and
// invite flows. It is deliberately not JWT: tokens are app-internal and opaque, with no
// algorithm negotiation.
//
//	type Reset struct{ UserID string `json:"uid"` }
//	codec, _ := token.New[Reset](key, token.WithTTL(15*time.Minute), token.WithPurpose("pwreset"))
//	tok, _   := codec.Issue(Reset{UserID: "u_123"})
//	got, err := codec.Parse(tok) // got.UserID == "u_123"
//
// The wire form is base64url(payload-envelope) signed with package sign; WithEncrypt adds
// payload encryption via package secret. Each token carries a random nonce, so identical
// payloads produce distinct tokens. Parse verifies the signature first (constant time),
// then checks expiry against the injected clock, then purpose. Errors are ErrExpired,
// ErrBadSignature, ErrMalformed, and ErrWrongPurpose.
package token
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -race -cover ./token/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add token/
git commit -m "feat(token): add generic opaque signed/encrypted expiring token codec"
```

---

## Task 12: Full-tree verification

**Files:** none (verification + any lint-driven fixups).

**Interfaces:** n/a.

- [ ] **Step 1: Run the formatter**

Run: `just fmt`
Expected: `gofmt`, `goimports`, and `betteralign -apply` run clean. `betteralign` may reorder struct fields in `clock.Mock`, `kdf.Params`, the various `config`/`Signer`/`Box`/`Codec` structs — that is expected; re-run `go test ./...` after if any file changed.

- [ ] **Step 2: Run the linters**

Run: `just lint`
Expected: `go vet`, `go build`, `golangci-lint`, `nilaway`, `betteralign`, and `modernize` all pass with no findings. If `golangci-lint`'s `err113` flags the bcrypt `==` comparison in `password/password.go`, change `errIsBcryptMismatch` to `errors.Is(e, bcrypt.ErrMismatchedHashAndPassword)` (add `"errors"` to imports), then re-run.

- [ ] **Step 3: Run the full test suite with race + coverage**

Run: `just test`
Expected: all packages PASS; the eleven new packages report coverage. No `-race` warnings.

- [ ] **Step 4: Confirm dependency surface**

Run: `go mod tidy && git diff --stat go.mod go.sum`
Expected: `golang.org/x/crypto` is in the direct `require` block (no `// indirect`); no other new direct dependency was added.

- [ ] **Step 5: Commit any fixups**

```bash
git add -A
git commit -m "chore(crypto): formatting, lint fixups, and module tidy for P1 crypto layer"
```

(If Steps 1–4 produced no changes, skip this commit.)

---

## Self-Review

**1. Spec coverage** — every spec section maps to a task:

| Spec item | Task |
|---|---|
| `clock` (Now-only, System, Mock) | 1 |
| `randx` (Bytes panic, Read, Hex, URLSafe, Int) | 2 |
| `subtlex` (length-safe constant-time compare) | 3 |
| `hashx` (SHA-256/512, HMAC, FileSHA256; no MD5/SHA1) | 4 |
| `redact` (Secret[T] + String/Map) | 5 |
| `kdf` (HKDF + Argon2id DeriveKey + Params) | 6 |
| `keyset` (versioned keyring, base64 env, All) | 7 |
| `sign` (HMAC, FromKeyset rotation, String forms) | 8 |
| `password` (Argon2id default, bcrypt fallback, needsRehash, PHC) | 9 |
| `secret` (AES-GCM default, WithChaCha, AAD, FromKeyset rotation, version byte) | 10 |
| `token` (generic Codec[T], TTL/purpose/encrypt/clock, not JWT) | 11 |
| Approach A key wiring (raw New + FromKeyset) | 8, 10, 11 |
| `x/crypto` indirect→direct, confined | 6, 12 |
| Build-order waves | task ordering 1→11 |
| Black-box tests, constant-time via subtlex | all test steps; 3 consumed by 8/9 |
| Anti-scope (no JWT, no KMS, no scrypt, Now-only) | enforced by what is built |

No gaps.

**2. Placeholder scan** — searched for TBD/TODO/"add error handling"/"similar to". None present; every code and test step contains complete, compilable content.

**3. Type consistency** — verified cross-task names and signatures: `kdf.Params{Time,Memory,KeyLen,Threads}` defined in Task 6 is consumed verbatim by `password` (Task 9, `WithArgon2Params(kdf.Params)`); `keyset.New/Primary/ByVersion` (Task 7) are consumed by `sign.FromKeyset` (Task 8), `secret.FromKeyset` (Task 10), `token.FromKeyset` (Task 11); `subtlex.BytesEqual` (Task 3) is consumed by `sign` (8) and `password` (9); `secret.Box` (Task 10) is consumed by `token.WithEncrypt` (Task 11); `clock.Clock`/`NewMock` (Task 1) are consumed by `token.WithClock` and its tests (Task 11); `sign.Signer.SignString` returns `"<ver>.<mac>"` and `token.Issue`/`Parse` (Task 11) account for that exact format. Consistent throughout.

Two intentional deviations from the spec's dependency sketch, both noted in their tasks:
- `keyset` is stdlib-only (no `subtlex`) because it performs no secret comparison (Task 7).
- `randx` is imported by `password` (salt) and `secret` (nonce) and `token` (nonce); the spec implies this via "Nonces come from randx" / Argon2 salt — consistent.
