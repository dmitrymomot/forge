# Web-Boundary Security Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the web-boundary security bundle — `web/cookie`, `web/csrf`, `web/secheaders`, `web/cors`, `web/timeout`, `web/compress` — plus the repo-wide env-prefix retag, per the approved spec at `docs/superpowers/specs/2026-07-06-web-boundary-security-bundle-design.md`.

**Architecture:** Six single-responsibility packages under `web/`. One new internal edge: `csrf` composes `web/cookie` (signed double-submit token). Everything else rides shipped seams: `middleware.Middleware`, `problem.Responder`, `ctxkey.Key[T]`, `crypto/{keyset,sign,secret}`, `core/random`. The PR's first commit retags 8 existing Config structs with baked env prefixes (new repo convention).

**Tech Stack:** Go stdlib + shipped forge packages only. Zero new module dependencies.

## Global Constraints

- Work ONLY in the current branch `claude/competent-gauss-cebdf3`; never switch.
- Zero new entries in `go.mod`.
- Options pattern (`type Option func(*config)`) — NEVER builders.
- Black-box tests only: test files in `package <name>_test`.
- Package anatomy: `doc.go` (package comment + runnable example) · `config.go` (env-loadable packages) · `options.go` · `errors.go` (single-line, `errors.Is`-matchable sentinels) · impl.
- Defaults live in `DefaultConfig()` only — NO `default=` env-tag options.
- Env tags carry the baked package prefix (`COOKIE_KEYS`, not `KEYS`).
- Format with `just fmt ./web/cookie/...` (package-path form; single-file form trips betteralign).
- After each task: `just lint` and `go test -race ./<pkg>/...` must pass.
- Go 1.26 idioms: `new(expr)` allowed; run modernize via `just lint`.
- Commit messages: conventional commits. NO Claude/AI attribution lines of any kind.
- Constructors of env-loadable middleware: `New(opts ...Option) (middleware.Middleware, error)` with `WithConfig(Config)` applied first-wins-last semantics (later options override). `cookie` alone takes the keyset positionally.

**Existing APIs consumed (verified against HEAD):**

- `keyset.New(...keyset.Option) (*keyset.Keyset, error)`; `keyset.WithPrimary(version int, key []byte)`, `keyset.WithBase64Keys(s string)` (comma-separated `version:stdBase64`, highest version = primary).
- `sign.FromKeyset(ks *keyset.Keyset, opts ...sign.Option) (*sign.Signer, error)`; `(*Signer).SignString(msg string) string` → `"<version>.<b64url mac>"`; `(*Signer).VerifyString(msg, signed string) bool` (constant-time).
- `secret.FromKeyset(ks *keyset.Keyset, opts ...secret.Option) (*secret.Box, error)`; `secret.WithAAD(aad []byte)`; `(*Box).EncryptString/DecryptString(string) (string, error)`; decrypt failure → `secret.ErrDecryptFailed`. Keys must be 32 bytes.
- `middleware.Middleware = func(http.Handler) http.Handler`; `middleware.Chain`, `middleware.Wrap`, `middleware.When/Skip(pred, mw)`; `middleware.WrapWriter(w) middleware.ResponseWriter` with `Status() int`, `Written() int64`, `Wrote() bool`, `Unwrap() http.ResponseWriter`.
- `problem.Responder = func(http.ResponseWriter, *http.Request, error)`; `problem.JSON(opts ...problem.Option) problem.Responder`; `problem.WithStatus(code int)`.
- `ctxkey.New[T](name string) ctxkey.Key[T]`; `(Key[T]).With(ctx, v) context.Context`; `(Key[T]).From(ctx) (T, bool)`.
- `random.URLSafe(n int) string` (crypto/rand, unpadded base64url of n bytes); `random.Bytes(n int) []byte`.
- `ops/config.Populate(dst any, opts ...config.Option) error` — untagged nested struct keeps parent prefix; tagged nest prepends `TAG_`; absent env keys leave fields untouched; auto-runs `Validate()` if implemented.

---

### Task 1: Env-prefix retag of 8 existing Config structs

**Files:**
- Modify: `web/httpserver/config.go`
- Modify: `ops/logger/config.go`
- Modify: `ops/logger/sentry/config.go`
- Modify: `ops/supervisor/config.go`
- Modify: `data/postgres/config.go`
- Modify: `data/redis/config.go`
- Modify: `data/mongo/config.go`
- Modify: `data/opensearch/config.go`
- Modify: any `doc.go` / `*_test.go` / `examples/` file referencing the old env names (found by grep in Step 1).

**Interfaces:**
- Consumes: nothing.
- Produces: the baked-prefix env convention every later task follows. No Go identifiers change — tags and docs only.

- [ ] **Step 1: Find every reference to the old env names**

Run:
```bash
cd /Users/dmitrymomot/Dev/claude_worktrees/forge/competent-gauss-cebdf3
grep -rn --include='*.go' -E 'env:"(ADDR|NAME|TLS_CERT_FILE|TLS_KEY_FILE|SHUTDOWN_TIMEOUT|READ_HEADER_TIMEOUT|READ_TIMEOUT|WRITE_TIMEOUT|IDLE_TIMEOUT|MAX_HEADER_BYTES|LEVEL|FORMAT|FILE|ADD_SOURCE|DSN|ENVIRONMENT|MIN_LEVEL|ENABLE_LOGS|RECOVER|MASTER_NAME|USERNAME|PASSWORD|ADDRESSES|DB|POOL_SIZE|MIN_IDLE_CONNS|DIAL_TIMEOUT|CONN_MAX_IDLE_TIME|RETRY_ATTEMPTS|RETRY_INTERVAL|MAX_RETRIES|REQUEST_TIMEOUT|INSECURE_SKIP_VERIFY|URL|MAX_CONN_LIFETIME|MAX_CONN_IDLE_TIME|HEALTH_CHECK_PERIOD|CONNECT_TIMEOUT|MIN_CONNS|MAX_CONNS|URI|DATABASE|READ_PREFERENCE|READ_CONCERN|WRITE_CONCERN|MAX_POOL_SIZE|MIN_POOL_SIZE|SERVER_SELECTION_TIMEOUT)"' web ops data
# and usages in tests/docs/examples:
grep -rn --include='*.go' -E 't\.Setenv\("|WithLookup' web/httpserver ops/logger ops/supervisor data examples 2>/dev/null
grep -rn -E '\b(SHUTDOWN_TIMEOUT|READ_TIMEOUT|LOG?_?LEVEL|DSN|ADDRESSES)\b' examples docs/superpowers 2>/dev/null | grep -v specs/ | grep -v plans/
```
Expected: hits in the 8 config.go files, possibly their doc.go and tests. Record the full list; every hit gets updated in Step 2.

- [ ] **Step 2: Retag each config struct**

Apply exactly these renames (tag strings only — field names, types, comments stay):

| File | Old → New |
|---|---|
| `web/httpserver/config.go` | `ADDR`→`SERVER_ADDR`, `NAME`→`SERVER_NAME`, `TLS_CERT_FILE`→`SERVER_TLS_CERT_FILE`, `TLS_KEY_FILE`→`SERVER_TLS_KEY_FILE`, `SHUTDOWN_TIMEOUT`→`SERVER_SHUTDOWN_TIMEOUT`, `READ_HEADER_TIMEOUT`→`SERVER_READ_HEADER_TIMEOUT`, `READ_TIMEOUT`→`SERVER_READ_TIMEOUT`, `WRITE_TIMEOUT`→`SERVER_WRITE_TIMEOUT`, `IDLE_TIMEOUT`→`SERVER_IDLE_TIMEOUT`, `MAX_HEADER_BYTES`→`SERVER_MAX_HEADER_BYTES` |
| `ops/logger/config.go` | `LEVEL`→`LOG_LEVEL`, `FORMAT`→`LOG_FORMAT`, `FILE`→`LOG_FILE`, `ADD_SOURCE`→`LOG_ADD_SOURCE` |
| `ops/logger/sentry/config.go` | `DSN`→`SENTRY_DSN`, `ENVIRONMENT`→`SENTRY_ENVIRONMENT`, `MIN_LEVEL`→`SENTRY_MIN_LEVEL`, `ENABLE_LOGS`→`SENTRY_ENABLE_LOGS` |
| `ops/supervisor/config.go` | `SHUTDOWN_TIMEOUT`→`SUPERVISOR_SHUTDOWN_TIMEOUT`, `RECOVER`→`SUPERVISOR_RECOVER` |
| `data/postgres/config.go` | `URL`→`DB_URL`, `MAX_CONN_LIFETIME`→`DB_MAX_CONN_LIFETIME`, `MAX_CONN_IDLE_TIME`→`DB_MAX_CONN_IDLE_TIME`, `HEALTH_CHECK_PERIOD`→`DB_HEALTH_CHECK_PERIOD`, `CONNECT_TIMEOUT`→`DB_CONNECT_TIMEOUT`, `RETRY_INTERVAL`→`DB_RETRY_INTERVAL`, `MIN_CONNS`→`DB_MIN_CONNS`, `MAX_CONNS`→`DB_MAX_CONNS`, `RETRY_ATTEMPTS`→`DB_RETRY_ATTEMPTS` |
| `data/redis/config.go` | prefix every tag with `REDIS_` (`MASTER_NAME`→`REDIS_MASTER_NAME`, `USERNAME`→`REDIS_USERNAME`, `PASSWORD`→`REDIS_PASSWORD`, `ADDRESSES`→`REDIS_ADDRESSES`, `DB`→`REDIS_DB`, `POOL_SIZE`→`REDIS_POOL_SIZE`, `MIN_IDLE_CONNS`→`REDIS_MIN_IDLE_CONNS`, `DIAL_TIMEOUT`→`REDIS_DIAL_TIMEOUT`, `READ_TIMEOUT`→`REDIS_READ_TIMEOUT`, `WRITE_TIMEOUT`→`REDIS_WRITE_TIMEOUT`, `CONN_MAX_IDLE_TIME`→`REDIS_CONN_MAX_IDLE_TIME`, `RETRY_ATTEMPTS`→`REDIS_RETRY_ATTEMPTS`, `RETRY_INTERVAL`→`REDIS_RETRY_INTERVAL`) |
| `data/mongo/config.go` | prefix every tag with `MONGO_` (`URI`, `DATABASE`, `READ_PREFERENCE`, `READ_CONCERN`, `WRITE_CONCERN`, `MAX_POOL_SIZE`, `MIN_POOL_SIZE`, `CONNECT_TIMEOUT`, `SERVER_SELECTION_TIMEOUT`, `MAX_CONN_IDLE_TIME`, `RETRY_INTERVAL`, `RETRY_ATTEMPTS`) |
| `data/opensearch/config.go` | prefix every tag with `OPENSEARCH_` (`USERNAME`, `PASSWORD`, `ADDRESSES`, `MAX_RETRIES`, `REQUEST_TIMEOUT`, `RETRY_ATTEMPTS`, `RETRY_INTERVAL`, `INSECURE_SKIP_VERIFY`) |

Update every doc.go/example/test hit from Step 1 to the new names (e.g. a doc comment showing `SHUTDOWN_TIMEOUT=5s` under supervisor becomes `SUPERVISOR_SHUTDOWN_TIMEOUT=5s`).

- [ ] **Step 3: Verify the tree still builds and tests pass**

Run: `go build ./... && go test ./web/httpserver/... ./ops/... ./data/... && just lint`
Expected: all green. If a test fails, it references an old env name — fix the test, not the tag.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor(config): bake package prefixes into env tags (SERVER_, LOG_, SENTRY_, SUPERVISOR_, DB_, REDIS_, MONGO_, OPENSEARCH_)"
```

---

### Task 2: web/cookie — signed + encrypted cookie codec

**Files:**
- Create: `web/cookie/doc.go`
- Create: `web/cookie/config.go`
- Create: `web/cookie/options.go`
- Create: `web/cookie/errors.go`
- Create: `web/cookie/codec.go`
- Test: `web/cookie/codec_test.go`, `web/cookie/config_test.go`

**Interfaces:**
- Consumes: `crypto/keyset`, `crypto/sign`, `crypto/secret` (signatures in Global Constraints).
- Produces (Task 3 relies on these exact signatures):
  - `func New(ks *keyset.Keyset, opts ...Option) (*Codec, error)`
  - `func FromConfig(cfg Config, opts ...Option) (*Codec, error)`
  - `func (c *Codec) Set(w http.ResponseWriter, name, value string, opts ...WriteOption) error`
  - `func (c *Codec) Get(r *http.Request, name string) (string, error)`
  - `func (c *Codec) SetSigned(w http.ResponseWriter, name, value string, opts ...WriteOption) error`
  - `func (c *Codec) GetSigned(r *http.Request, name string) (string, error)`
  - `func (c *Codec) SetEncrypted(w http.ResponseWriter, name, value string, opts ...WriteOption) error`
  - `func (c *Codec) GetEncrypted(r *http.Request, name string) (string, error)`
  - `func (c *Codec) Delete(w http.ResponseWriter, name string)`
  - `func (c *Codec) SupportsHostPrefix() bool` — true when policy is Secure + Path=/ + no Domain
  - Sentinels: `ErrInvalidCookie`, `ErrTooLarge`, `ErrInvalidConfig`

- [ ] **Step 1: Write the failing tests**

`web/cookie/codec_test.go`:

```go
package cookie_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/cookie"
)

func newCodec(t *testing.T, opts ...cookie.Option) *cookie.Codec {
	t.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cookie.New(ks, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// roundTrip writes via set, then builds a request carrying the response cookies.
func roundTrip(t *testing.T, set func(w http.ResponseWriter) error) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := set(rec); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range rec.Result().Cookies() {
		r.AddCookie(ck)
	}
	return r
}

func TestPlainRoundTripCarriesPolicy(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.Set(rec, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	cks := rec.Result().Cookies()
	if len(cks) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cks))
	}
	ck := cks[0]
	if ck.Value != "dark" || !ck.Secure || !ck.HttpOnly || ck.Path != "/" || ck.SameSite != http.SameSiteLaxMode {
		t.Fatalf("policy flags not applied: %+v", ck)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(ck)
	got, err := c.Get(r, "theme")
	if err != nil || got != "dark" {
		t.Fatalf("Get = %q, %v", got, err)
	}
}

func TestSignedRoundTrip(t *testing.T) {
	c := newCodec(t)
	r := roundTrip(t, func(w http.ResponseWriter) error { return c.SetSigned(w, "sid", "hello world") })
	got, err := c.GetSigned(r, "sid")
	if err != nil || got != "hello world" {
		t.Fatalf("GetSigned = %q, %v", got, err)
	}
}

func TestSignedTamperRejected(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "sid", "value"); err != nil {
		t.Fatal(err)
	}
	ck := rec.Result().Cookies()[0]
	ck.Value = strings.Replace(ck.Value, string(ck.Value[0]), "x", 1)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(ck)
	if _, err := c.GetSigned(r, "sid"); !errors.Is(err, cookie.ErrInvalidCookie) {
		t.Fatalf("want ErrInvalidCookie, got %v", err)
	}
}

func TestSignedNameBindingRejectsReplay(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "a", "value"); err != nil {
		t.Fatal(err)
	}
	stolen := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "b", Value: stolen.Value})
	if _, err := c.GetSigned(r, "b"); !errors.Is(err, cookie.ErrInvalidCookie) {
		t.Fatalf("cookie minted for a must not verify as b, got %v", err)
	}
}

func TestEncryptedRoundTripAndOpacity(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetEncrypted(rec, "sess", "secret-data"); err != nil {
		t.Fatal(err)
	}
	ck := rec.Result().Cookies()[0]
	if strings.Contains(ck.Value, "secret-data") {
		t.Fatal("encrypted cookie leaks plaintext")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(ck)
	got, err := c.GetEncrypted(r, "sess")
	if err != nil || got != "secret-data" {
		t.Fatalf("GetEncrypted = %q, %v", got, err)
	}
}

func TestEncryptedNameBindingRejectsReplay(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.SetEncrypted(rec, "a", "value"); err != nil {
		t.Fatal(err)
	}
	stolen := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "b", Value: stolen.Value})
	if _, err := c.GetEncrypted(r, "b"); !errors.Is(err, cookie.ErrInvalidCookie) {
		t.Fatalf("ciphertext minted for a must not decrypt as b, got %v", err)
	}
}

func TestRotationRetiredKeyStillReads(t *testing.T) {
	old := make([]byte, 32)
	newKey := make([]byte, 32)
	newKey[0] = 1
	ks1, _ := keyset.New(keyset.WithPrimary(1, old))
	c1, err := cookie.New(ks1)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := c1.SetSigned(rec, "sid", "v"); err != nil {
		t.Fatal(err)
	}
	if err := c1.SetEncrypted(rec, "sess", "w"); err != nil {
		t.Fatal(err)
	}
	ks2, _ := keyset.New(keyset.WithPrimary(2, newKey), keyset.WithRetired(1, old))
	c2, err := cookie.New(ks2)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range rec.Result().Cookies() {
		r.AddCookie(ck)
	}
	if got, err := c2.GetSigned(r, "sid"); err != nil || got != "v" {
		t.Fatalf("rotated GetSigned = %q, %v", got, err)
	}
	if got, err := c2.GetEncrypted(r, "sess"); err != nil || got != "w" {
		t.Fatalf("rotated GetEncrypted = %q, %v", got, err)
	}
}

func TestMissingCookieIsInvalid(t *testing.T) {
	c := newCodec(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, get := range []func() (string, error){
		func() (string, error) { return c.Get(r, "nope") },
		func() (string, error) { return c.GetSigned(r, "nope") },
		func() (string, error) { return c.GetEncrypted(r, "nope") },
	} {
		if _, err := get(); !errors.Is(err, cookie.ErrInvalidCookie) {
			t.Fatalf("want ErrInvalidCookie, got %v", err)
		}
	}
}

func TestHostPrefixEnforcement(t *testing.T) {
	c := newCodec(t, cookie.WithSecure(false))
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "__Host-x", "v"); !errors.Is(err, cookie.ErrInvalidConfig) {
		t.Fatalf("__Host- without Secure must fail, got %v", err)
	}
	if newCodec(t).SupportsHostPrefix() != true {
		t.Fatal("default policy should support __Host-")
	}
	if c.SupportsHostPrefix() {
		t.Fatal("insecure policy must not support __Host-")
	}
}

func TestTooLarge(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	big := strings.Repeat("a", 5000)
	if err := c.Set(rec, "big", big); !errors.Is(err, cookie.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestWriteOptionOverrides(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	if err := c.Set(rec, "flash", "hi", cookie.WithWriteMaxAge(time.Minute), cookie.WithWritePath("/admin")); err != nil {
		t.Fatal(err)
	}
	ck := rec.Result().Cookies()[0]
	if ck.MaxAge != 60 || ck.Path != "/admin" {
		t.Fatalf("write overrides not applied: %+v", ck)
	}
}

func TestDeleteExpires(t *testing.T) {
	c := newCodec(t)
	rec := httptest.NewRecorder()
	c.Delete(rec, "sid")
	ck := rec.Result().Cookies()[0]
	if ck.MaxAge != -1 || ck.Value != "" {
		t.Fatalf("Delete must expire the cookie: %+v", ck)
	}
}
```

`web/cookie/config_test.go`:

```go
package cookie_test

import (
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/cookie"
)

func TestFromConfigRoundTrip(t *testing.T) {
	keys := "1:" + base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := cookie.DefaultConfig()
	cfg.Keys = keys
	c, err := cookie.FromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := c.SetSigned(rec, "sid", "v"); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := cookie.DefaultConfig()
	cfg.SameSite = "bogus"
	if err := cfg.Validate(); !errors.Is(err, cookie.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
	none := cookie.DefaultConfig()
	none.SameSite = "none"
	none.Secure = false
	if err := none.Validate(); !errors.Is(err, cookie.ErrInvalidConfig) {
		t.Fatalf("SameSite=none without Secure must fail, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/cookie/...`
Expected: FAIL — package does not exist / undefined identifiers.

- [ ] **Step 3: Implement the package**

`web/cookie/errors.go`:

```go
package cookie

import "errors"

var (
	// ErrInvalidCookie covers absent, tampered, or undecryptable cookies. One
	// error for all three so callers can't build a padding/absence oracle.
	ErrInvalidCookie = errors.New("cookie: invalid cookie")
	// ErrTooLarge means the encoded Set-Cookie header exceeds 4096 bytes,
	// which browsers truncate or drop silently.
	ErrTooLarge = errors.New("cookie: encoded cookie exceeds 4096 bytes")
	// ErrInvalidConfig covers bad construction input and policy violations
	// such as a __Host- name with an incompatible policy.
	ErrInvalidConfig = errors.New("cookie: invalid config")
)
```

`web/cookie/config.go`:

```go
package cookie

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config is the env-loadable cookie policy. Key material rides Keys in
// keyset.WithBase64Keys format ("version:base64,..."). Defaults live in
// DefaultConfig; the canonical loading flow preserves them:
//
//	cfg := cookie.DefaultConfig()
//	err := appconfig.Populate(&cfg)
type Config struct {
	Keys     string        `env:"COOKIE_KEYS"`
	Path     string        `env:"COOKIE_PATH"`
	Domain   string        `env:"COOKIE_DOMAIN"`
	MaxAge   time.Duration `env:"COOKIE_MAX_AGE"`
	SameSite string        `env:"COOKIE_SAME_SITE"` // lax | strict | none
	Secure   bool          `env:"COOKIE_SECURE"`
	HTTPOnly bool          `env:"COOKIE_HTTP_ONLY"`
}

// DefaultConfig returns the secure-by-default policy: Path=/, SameSite=lax,
// Secure and HTTPOnly on, session-lifetime cookies.
func DefaultConfig() Config {
	return Config{Path: "/", SameSite: "lax", Secure: true, HTTPOnly: true}
}

// Validate checks enum fields and the SameSite=none + Secure interaction.
func (c Config) Validate() error {
	if _, err := parseSameSite(c.SameSite); err != nil {
		return err
	}
	if strings.EqualFold(c.SameSite, "none") && !c.Secure {
		return fmt.Errorf("%w: SameSite=none requires Secure", ErrInvalidConfig)
	}
	if c.Path != "" && !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("%w: path %q must start with /", ErrInvalidConfig, c.Path)
	}
	return nil
}

func parseSameSite(s string) (http.SameSite, error) {
	switch strings.ToLower(s) {
	case "", "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	default:
		return 0, fmt.Errorf("%w: unknown SameSite %q", ErrInvalidConfig, s)
	}
}
```

`web/cookie/options.go`:

```go
package cookie

import (
	"net/http"
	"time"
)

type policy struct {
	path     string
	domain   string
	maxAge   time.Duration
	sameSite http.SameSite
	secure   bool
	httpOnly bool
}

// Option configures the codec-wide policy.
type Option func(*policy)

// WithPath sets the default cookie path (default "/").
func WithPath(p string) Option { return func(c *policy) { c.path = p } }

// WithDomain sets the cookie domain (default host-only).
func WithDomain(d string) Option { return func(c *policy) { c.domain = d } }

// WithMaxAge sets the default lifetime; 0 means session cookie.
func WithMaxAge(d time.Duration) Option { return func(c *policy) { c.maxAge = d } }

// WithSameSite sets the default SameSite mode (default Lax).
func WithSameSite(s http.SameSite) Option { return func(c *policy) { c.sameSite = s } }

// WithSecure toggles the Secure flag (default true).
func WithSecure(v bool) Option { return func(c *policy) { c.secure = v } }

// WithHTTPOnly toggles the HttpOnly flag (default true).
func WithHTTPOnly(v bool) Option { return func(c *policy) { c.httpOnly = v } }

// WriteOption overrides the policy for a single Set call.
type WriteOption func(*policy)

// WithWriteMaxAge overrides the lifetime for this write; negative expires now.
func WithWriteMaxAge(d time.Duration) WriteOption { return func(c *policy) { c.maxAge = d } }

// WithWritePath overrides the path for this write.
func WithWritePath(p string) WriteOption { return func(c *policy) { c.path = p } }

// WithWriteDomain overrides the domain for this write.
func WithWriteDomain(d string) WriteOption { return func(c *policy) { c.domain = d } }

// WithWriteSameSite overrides the SameSite mode for this write.
func WithWriteSameSite(s http.SameSite) WriteOption { return func(c *policy) { c.sameSite = s } }
```

`web/cookie/codec.go`:

```go
package cookie

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/secret"
	"github.com/dmitrymomot/forge/crypto/sign"
)

// maxEncodedLen is the practical Set-Cookie size limit shared by browsers.
const maxEncodedLen = 4096

const hostPrefix = "__Host-"

// Codec writes and reads plain, signed, and encrypted cookies under one
// policy. Signed = tamper-proof but client-readable (HMAC). Encrypted =
// tamper-proof AND opaque (AEAD; the auth tag makes a separate signature
// redundant).
type Codec struct {
	ks     *keyset.Keyset
	signer *sign.Signer
	boxes  sync.Map // cookie name -> *secret.Box, AAD-bound to the name
	pol    policy
}

// New builds a Codec over ks. Signing and encryption keys derive from the
// same keyset, so rotation is one operation.
func New(ks *keyset.Keyset, opts ...Option) (*Codec, error) {
	if ks == nil {
		return nil, fmt.Errorf("%w: nil keyset", ErrInvalidConfig)
	}
	pol := policy{path: "/", sameSite: http.SameSiteLaxMode, secure: true, httpOnly: true}
	for _, o := range opts {
		o(&pol)
	}
	if pol.sameSite == http.SameSiteNoneMode && !pol.secure {
		return nil, fmt.Errorf("%w: SameSite=none requires Secure", ErrInvalidConfig)
	}
	signer, err := sign.FromKeyset(ks)
	if err != nil {
		return nil, err
	}
	return &Codec{ks: ks, signer: signer, pol: pol}, nil
}

// FromConfig builds the keyset from cfg.Keys and applies the policy fields.
// Empty Path/SameSite normalize to the defaults; opts apply last and win.
func FromConfig(cfg Config, opts ...Option) (*Codec, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ks, err := keyset.New(keyset.WithBase64Keys(cfg.Keys))
	if err != nil {
		return nil, err
	}
	sameSite, err := parseSameSite(cfg.SameSite)
	if err != nil {
		return nil, err
	}
	path := cfg.Path
	if path == "" {
		path = "/"
	}
	base := []Option{
		WithPath(path),
		WithDomain(cfg.Domain),
		WithMaxAge(cfg.MaxAge),
		WithSameSite(sameSite),
		WithSecure(cfg.Secure),
		WithHTTPOnly(cfg.HTTPOnly),
	}
	return New(ks, append(base, opts...)...)
}

// SupportsHostPrefix reports whether the codec policy satisfies the __Host-
// cookie-prefix rules (Secure, Path=/, host-only).
func (c *Codec) SupportsHostPrefix() bool {
	return c.pol.secure && c.pol.path == "/" && c.pol.domain == ""
}

// Set writes a plain cookie with the codec policy applied. Use it for
// non-sensitive values so the app never mixes stdlib http.SetCookie calls
// (with forgotten flags) into a codec-managed cookie surface.
func (c *Codec) Set(w http.ResponseWriter, name, value string, opts ...WriteOption) error {
	return c.write(w, name, value, opts)
}

// Get reads a plain cookie. Absent cookies return ErrInvalidCookie, matching
// the signed/encrypted paths.
func (c *Codec) Get(r *http.Request, name string) (string, error) {
	ck, err := r.Cookie(name)
	if err != nil {
		return "", ErrInvalidCookie
	}
	return ck.Value, nil
}

// SetSigned writes value with an HMAC bound to the cookie name, so a value
// minted for one cookie cannot be replayed as another.
func (c *Codec) SetSigned(w http.ResponseWriter, name, value string, opts ...WriteOption) error {
	mac := c.signer.SignString(bindName(name, value))
	encoded := base64.RawURLEncoding.EncodeToString([]byte(value)) + "." + mac
	return c.write(w, name, encoded, opts)
}

// GetSigned reads and verifies a signed cookie. Any failure — absent,
// malformed, bad signature, wrong name — returns ErrInvalidCookie.
func (c *Codec) GetSigned(r *http.Request, name string) (string, error) {
	ck, err := r.Cookie(name)
	if err != nil {
		return "", ErrInvalidCookie
	}
	rawValue, mac, ok := strings.Cut(ck.Value, ".")
	if !ok {
		return "", ErrInvalidCookie
	}
	vb, err := base64.RawURLEncoding.DecodeString(rawValue)
	if err != nil {
		return "", ErrInvalidCookie
	}
	value := string(vb)
	if !c.signer.VerifyString(bindName(name, value), mac) {
		return "", ErrInvalidCookie
	}
	return value, nil
}

// SetEncrypted writes value AEAD-encrypted with the cookie name as AAD.
func (c *Codec) SetEncrypted(w http.ResponseWriter, name, value string, opts ...WriteOption) error {
	box, err := c.boxFor(name)
	if err != nil {
		return err
	}
	enc, err := box.EncryptString(value)
	if err != nil {
		return err
	}
	return c.write(w, name, enc, opts)
}

// GetEncrypted reads and decrypts an encrypted cookie. Any failure returns
// ErrInvalidCookie.
func (c *Codec) GetEncrypted(r *http.Request, name string) (string, error) {
	ck, err := r.Cookie(name)
	if err != nil {
		return "", ErrInvalidCookie
	}
	box, err := c.boxFor(name)
	if err != nil {
		return "", err
	}
	value, err := box.DecryptString(ck.Value)
	if err != nil {
		return "", ErrInvalidCookie
	}
	return value, nil
}

// Delete expires the named cookie under the codec policy's path/domain.
func (c *Codec) Delete(w http.ResponseWriter, name string) {
	ck := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     c.pol.path,
		Domain:   c.pol.domain,
		MaxAge:   -1,
		Secure:   c.pol.secure,
		HttpOnly: c.pol.httpOnly,
		SameSite: c.pol.sameSite,
	}
	if strings.HasPrefix(name, hostPrefix) {
		ck.Path = "/"
		ck.Domain = ""
	}
	http.SetCookie(w, ck)
}

func bindName(name, value string) string { return name + "\x00" + value }

func (c *Codec) boxFor(name string) (*secret.Box, error) {
	if b, ok := c.boxes.Load(name); ok {
		return b.(*secret.Box), nil
	}
	b, err := secret.FromKeyset(c.ks, secret.WithAAD([]byte("forge/web/cookie:"+name)))
	if err != nil {
		return nil, err
	}
	actual, _ := c.boxes.LoadOrStore(name, b)
	return actual.(*secret.Box), nil
}

func (c *Codec) write(w http.ResponseWriter, name, value string, opts []WriteOption) error {
	pol := c.pol
	for _, o := range opts {
		o(&pol)
	}
	if strings.HasPrefix(name, hostPrefix) && (!pol.secure || pol.path != "/" || pol.domain != "") {
		return fmt.Errorf("%w: %s cookie requires Secure, Path=/, and no Domain", ErrInvalidConfig, hostPrefix)
	}
	ck := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     pol.path,
		Domain:   pol.domain,
		Secure:   pol.secure,
		HttpOnly: pol.httpOnly,
		SameSite: pol.sameSite,
	}
	switch {
	case pol.maxAge > 0:
		ck.MaxAge = int(pol.maxAge / time.Second)
	case pol.maxAge < 0:
		ck.MaxAge = -1
	}
	if err := ck.Valid(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCookie, err)
	}
	if encoded := ck.String(); len(encoded) > maxEncodedLen {
		return fmt.Errorf("%w: %d bytes", ErrTooLarge, len(encoded))
	}
	http.SetCookie(w, ck)
	return nil
}
```

`web/cookie/doc.go`:

```go
// Package cookie is a signed + encrypted cookie codec with secure defaults
// (HttpOnly, Secure, SameSite=Lax, __Host- support) and keyset rotation.
//
// Three security levels, chosen per call:
//   - Set/Get: plain value, policy flags still applied.
//   - SetSigned/GetSigned: HMAC, tamper-proof but client-readable.
//   - SetEncrypted/GetEncrypted: AEAD, tamper-proof AND opaque. The AEAD auth
//     tag already provides integrity — encrypted cookies are not additionally
//     signed because that would be pure overhead.
//
// Values are bound to their cookie name (MAC message / AEAD AAD), so a value
// minted for one cookie cannot be replayed under another name.
//
//	ks, _ := keyset.New(keyset.WithBase64Keys(os.Getenv("COOKIE_KEYS")))
//	codec, _ := cookie.New(ks)
//	_ = codec.SetSigned(w, "__Host-csrf", token)
//	_ = codec.SetEncrypted(w, "session", sid, cookie.WithWriteMaxAge(24*time.Hour))
package cookie
```

Include a runnable `Example` function in doc.go following the shipped packages' pattern (see `web/requestid/doc.go` for shape).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./web/cookie/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/cookie/...
just lint
git add web/cookie
git commit -m "feat(web/cookie): signed + encrypted cookie codec with keyset rotation and name binding"
```

---

### Task 3: web/csrf — stateless double-submit middleware

**Files:**
- Create: `web/csrf/doc.go`
- Create: `web/csrf/options.go`
- Create: `web/csrf/errors.go`
- Create: `web/csrf/csrf.go`
- Test: `web/csrf/csrf_test.go`

**Interfaces:**
- Consumes: `cookie.Codec` (Task 2 signatures, incl. `SupportsHostPrefix`), `middleware.Middleware`, `problem.JSON/WithStatus`, `ctxkey`, `random.URLSafe`.
- Produces:
  - `func New(codec *cookie.Codec, opts ...Option) middleware.Middleware` (panics on nil codec — programmer error)
  - `func Token(r *http.Request) string`
  - Options: `WithCookieName(string)`, `WithHeader(string)`, `WithFormField(string)`, `WithResponder(problem.Responder)`, `WithSkip(func(*http.Request) bool)`
  - Sentinels: `ErrTokenMissing`, `ErrTokenInvalid`

- [ ] **Step 1: Write the failing tests**

`web/csrf/csrf_test.go`:

```go
package csrf_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/csrf"
	"github.com/dmitrymomot/forge/web/middleware"
)

func newCodec(t *testing.T, opts ...cookie.Option) *cookie.Codec {
	t.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cookie.New(ks, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// getToken performs a GET and returns the minted token and its cookies.
func getToken(t *testing.T, h http.Handler) (string, []*http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d", rec.Code)
	}
	token := strings.TrimSpace(rec.Body.String())
	if token == "" {
		t.Fatal("no token exposed on minting request")
	}
	return token, rec.Result().Cookies()
}

func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, csrf.Token(r))
	})
}

func TestGetMintsCookieAndExposesToken(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	if len(cks) != 1 || cks[0].Name != "__Host-csrf" {
		t.Fatalf("want minted __Host-csrf cookie, got %+v", cks)
	}
	if token == "" {
		t.Fatal("Token(r) empty on minting request")
	}
}

func TestHostPrefixFallback(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t, cookie.WithSecure(false))))
	_, cks := getToken(t, h)
	if len(cks) != 1 || cks[0].Name != "csrf" {
		t.Fatalf("insecure codec must fall back to plain csrf name, got %+v", cks)
	}
}

func TestPostWithHeaderTokenPasses(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	for _, ck := range cks {
		r.AddCookie(ck)
	}
	r.Header.Set("X-CSRF-Token", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST with valid header token = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestPostWithFormTokenPasses(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	form := url.Values{"csrf_token": {token}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, ck := range cks {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST with valid form token = %d", rec.Code)
	}
}

func TestPostRejections(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)

	tests := []struct {
		name  string
		setup func(r *http.Request)
	}{
		{"no cookie no token", func(r *http.Request) {}},
		{"cookie but no echo", func(r *http.Request) {
			for _, ck := range cks {
				r.AddCookie(ck)
			}
		}},
		{"wrong token", func(r *http.Request) {
			for _, ck := range cks {
				r.AddCookie(ck)
			}
			r.Header.Set("X-CSRF-Token", "wrong-"+token)
		}},
		{"tampered cookie", func(r *http.Request) {
			bad := *cks[0]
			bad.Value = "x" + bad.Value[1:]
			r.AddCookie(&bad)
			r.Header.Set("X-CSRF-Token", token)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			tt.setup(r)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("want 403, got %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Fatalf("want problem+json rejection, got %q", ct)
			}
		})
	}
}

func TestSafeMethodsPass(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d, want 200", m, rec.Code)
		}
	}
}

func TestSkipPredicateBypasses(t *testing.T) {
	mw := csrf.New(newCodec(t), csrf.WithSkip(func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/webhooks/")
	}))
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), mw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("skipped route = %d, want 200", rec.Code)
	}
}

func TestTokenStableAcrossRequests(t *testing.T) {
	h := middleware.Wrap(handler(), csrf.New(newCodec(t)))
	token, cks := getToken(t, h)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range cks {
		r.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := strings.TrimSpace(rec.Body.String()); got != token {
		t.Fatalf("token must be stable across requests: %q != %q", got, token)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("valid cookie must not be re-minted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/csrf/...`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement the package**

`web/csrf/errors.go`:

```go
package csrf

import "errors"

var (
	// ErrTokenMissing means the request carried no token echo (header or form
	// field) or no valid token cookie existed for an unsafe method.
	ErrTokenMissing = errors.New("csrf: token missing")
	// ErrTokenInvalid means the echoed token did not match the cookie token.
	ErrTokenInvalid = errors.New("csrf: token invalid")
)
```

`web/csrf/options.go`:

```go
package csrf

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	cookieName string
	header     string
	formField  string
	responder  problem.Responder
	skip       func(*http.Request) bool
}

// Option configures the middleware.
type Option func(*config)

// WithCookieName overrides the token cookie name (default "__Host-csrf",
// falling back to "csrf" when the codec policy can't satisfy __Host-).
func WithCookieName(name string) Option { return func(c *config) { c.cookieName = name } }

// WithHeader overrides the token header name (default "X-CSRF-Token").
func WithHeader(name string) Option { return func(c *config) { c.header = name } }

// WithFormField overrides the form field name (default "csrf_token").
func WithFormField(name string) Option { return func(c *config) { c.formField = name } }

// WithResponder overrides the rejection response (default problem.JSON 403).
func WithResponder(r problem.Responder) Option {
	return func(c *config) {
		if r != nil {
			c.responder = r
		}
	}
}

// WithSkip exempts requests matching pred (e.g. webhook endpoints verified
// by signature instead).
func WithSkip(pred func(*http.Request) bool) Option { return func(c *config) { c.skip = pred } }
```

`web/csrf/csrf.go`:

```go
package csrf

import (
	"crypto/subtle"
	"mime"
	"net/http"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

var tokenKey = ctxkey.New[string]("csrf_token")

// tokenBytes is the entropy of a minted token (32 bytes, base64url-encoded).
const tokenBytes = 32

// New returns stateless double-submit CSRF middleware over codec. The token
// lives in a signed cookie; unsafe methods must echo it via header or form
// field. New panics if codec is nil — that is a wiring bug, not a runtime
// condition.
func New(codec *cookie.Codec, opts ...Option) middleware.Middleware {
	if codec == nil {
		panic("csrf: nil cookie codec")
	}
	cfg := config{
		cookieName: "__Host-csrf",
		header:     "X-CSRF-Token",
		formField:  "csrf_token",
		responder:  problem.JSON(problem.WithStatus(http.StatusForbidden)),
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.cookieName == "__Host-csrf" && !codec.SupportsHostPrefix() {
		cfg.cookieName = "csrf"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skip != nil && cfg.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			token, err := codec.GetSigned(r, cfg.cookieName)
			fresh := err != nil
			if fresh {
				token = random.URLSafe(tokenBytes)
				if werr := codec.SetSigned(w, cfg.cookieName, token); werr != nil {
					cfg.responder(w, r, werr)
					return
				}
			}
			r = r.WithContext(tokenKey.With(r.Context(), token))
			if safeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if fresh {
				// Unsafe request without a pre-existing valid cookie can never
				// have a matching echo.
				cfg.responder(w, r, ErrTokenMissing)
				return
			}
			echo := r.Header.Get(cfg.header)
			if echo == "" && isForm(r) {
				echo = r.PostFormValue(cfg.formField)
			}
			switch {
			case echo == "":
				cfg.responder(w, r, ErrTokenMissing)
			case subtle.ConstantTimeCompare([]byte(echo), []byte(token)) != 1:
				cfg.responder(w, r, ErrTokenInvalid)
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// Token returns the CSRF token for the current request — put it in a
// <meta name="csrf-token"> tag or an htmx hx-headers attribute. It returns
// "" outside the middleware.
func Token(r *http.Request) string {
	v, _ := tokenKey.From(r.Context())
	return v
}

func safeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

func isForm(r *http.Request) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}
```

`web/csrf/doc.go`: package comment covering the double-submit scheme, the
htmx recipe (`Token(r)` → `<meta name="csrf-token" content="...">` +
`hx-headers='{"X-CSRF-Token": "..."}'`), the non-goals (no per-request
rotation, no Origin fallback, no session binding — rotate-on-login deletes
the cookie), and a runnable example wiring `cookie.New` + `csrf.New` +
`middleware.Wrap`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./web/csrf/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/csrf/...
just lint
git add web/csrf
git commit -m "feat(web/csrf): stateless double-submit middleware over signed cookie"
```

---

### Task 4: web/secheaders — security headers + CSP nonce

**Files:**
- Create: `web/secheaders/doc.go`
- Create: `web/secheaders/config.go`
- Create: `web/secheaders/options.go`
- Create: `web/secheaders/errors.go`
- Create: `web/secheaders/csp.go`
- Create: `web/secheaders/secheaders.go`
- Test: `web/secheaders/secheaders_test.go`

**Interfaces:**
- Consumes: `middleware.Middleware`, `ctxkey`, `random.URLSafe`.
- Produces:
  - `func New(opts ...Option) (middleware.Middleware, error)`
  - `func Nonce(ctx context.Context) string`
  - `type Policy struct { DefaultSrc, ScriptSrc, StyleSrc, ImgSrc, ConnectSrc, FontSrc, ObjectSrc, FrameAncestors, BaseURI, FormAction []string; ReportURI string }`
  - Source constants: `Self`, `None`, `UnsafeInline`, `UnsafeEval`, `StrictDynamic`, `Data`, `Blob`
  - Options: `WithConfig(Config)`, `WithCSP(Policy)`, `WithNonce()`
  - `type Config` (env prefix `SECURITY_HEADERS_`), `DefaultConfig()`, `(Config).Validate() error`
  - Sentinel: `ErrInvalidConfig`

- [ ] **Step 1: Write the failing tests**

`web/secheaders/secheaders_test.go`:

```go
package secheaders_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/secheaders"
)

func serve(t *testing.T, mw middleware.Middleware, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	middleware.Wrap(h, mw).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec
}

func TestDefaultHeaders(t *testing.T) {
	mw, err := secheaders.New()
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {})
	want := map[string]string{
		"X-Content-Type-Options":      "nosniff",
		"Referrer-Policy":             "strict-origin-when-cross-origin",
		"X-Frame-Options":             "DENY",
		"Cross-Origin-Opener-Policy":  "same-origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must be off by default")
	}
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("CSP must be off by default")
	}
}

func TestHSTSFromConfig(t *testing.T) {
	cfg := secheaders.DefaultConfig()
	cfg.HSTSMaxAge = 180 * 24 * time.Hour
	cfg.HSTSIncludeSubdomains = true
	mw, err := secheaders.New(secheaders.WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {})
	got := rec.Header().Get("Strict-Transport-Security")
	if !strings.Contains(got, "max-age=15552000") || !strings.Contains(got, "includeSubDomains") {
		t.Fatalf("HSTS = %q", got)
	}
}

func TestFrameOptionsOff(t *testing.T) {
	cfg := secheaders.DefaultConfig()
	cfg.FrameOptions = "off"
	mw, err := secheaders.New(secheaders.WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {})
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Fatal("FrameOptions=off must suppress the header")
	}
}

func TestInvalidFrameOptions(t *testing.T) {
	cfg := secheaders.DefaultConfig()
	cfg.FrameOptions = "BOGUS"
	if _, err := secheaders.New(secheaders.WithConfig(cfg)); !errors.Is(err, secheaders.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCSPWithNonce(t *testing.T) {
	mw, err := secheaders.New(
		secheaders.WithCSP(secheaders.Policy{
			DefaultSrc: []string{secheaders.Self},
			ScriptSrc:  []string{secheaders.Self},
		}),
		secheaders.WithNonce(),
	)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	h := func(w http.ResponseWriter, r *http.Request) {
		n := secheaders.Nonce(r.Context())
		seen = append(seen, n)
		io.WriteString(w, n)
	}
	rec1 := serve(t, mw, h)
	rec2 := serve(t, mw, h)
	if seen[0] == "" || seen[0] == seen[1] {
		t.Fatalf("nonces must be unique per request: %q vs %q", seen[0], seen[1])
	}
	csp := rec1.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("CSP missing default-src: %q", csp)
	}
	if !strings.Contains(csp, "'nonce-"+seen[0]+"'") {
		t.Fatalf("CSP missing request nonce: %q", csp)
	}
}

func TestHandlerOverrideWins(t *testing.T) {
	mw, err := secheaders.New()
	if err != nil {
		t.Fatal(err)
	}
	rec := serve(t, mw, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	})
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("handler override lost: %q", got)
	}
}

func TestNonceOutsideMiddlewareIsEmpty(t *testing.T) {
	if secheaders.Nonce(httptest.NewRequest(http.MethodGet, "/", nil).Context()) != "" {
		t.Fatal("Nonce outside middleware must be empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/secheaders/...`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement the package**

`web/secheaders/errors.go`:

```go
package secheaders

import "errors"

// ErrInvalidConfig marks bad Config values (unknown FrameOptions, negative HSTS).
var ErrInvalidConfig = errors.New("secheaders: invalid config")
```

`web/secheaders/config.go`:

```go
package secheaders

import (
	"fmt"
	"time"
)

// Config is the env-loadable deployment policy. CSP is code-shaped and lives
// in options (WithCSP/WithNonce), not env.
type Config struct {
	HSTSMaxAge            time.Duration `env:"SECURITY_HEADERS_HSTS_MAX_AGE"` // 0 disables HSTS
	HSTSIncludeSubdomains bool          `env:"SECURITY_HEADERS_HSTS_SUBDOMAINS"`
	FrameOptions          string        `env:"SECURITY_HEADERS_FRAME_OPTIONS"` // DENY (default) | SAMEORIGIN | off
	CSPReportURI          string        `env:"SECURITY_HEADERS_CSP_REPORT_URI"`
}

// DefaultConfig returns FrameOptions=DENY with HSTS and CSP reporting off.
func DefaultConfig() Config {
	return Config{FrameOptions: "DENY"}
}

// Validate checks enum fields. An empty FrameOptions normalizes to DENY.
func (c Config) Validate() error {
	switch c.FrameOptions {
	case "", "DENY", "SAMEORIGIN", "off":
	default:
		return fmt.Errorf("%w: FrameOptions %q (want DENY, SAMEORIGIN, or off)", ErrInvalidConfig, c.FrameOptions)
	}
	if c.HSTSMaxAge < 0 {
		return fmt.Errorf("%w: negative HSTSMaxAge", ErrInvalidConfig)
	}
	return nil
}
```

`web/secheaders/csp.go`:

```go
package secheaders

import "strings"

// CSP source keywords and schemes.
const (
	Self          = "'self'"
	None          = "'none'"
	UnsafeInline  = "'unsafe-inline'"
	UnsafeEval    = "'unsafe-eval'"
	StrictDynamic = "'strict-dynamic'"
	Data          = "data:"
	Blob          = "blob:"
)

// Policy is a typed Content-Security-Policy. Only non-empty directives are
// emitted. When the middleware runs with WithNonce, the per-request nonce is
// appended to ScriptSrc and StyleSrc.
type Policy struct {
	DefaultSrc     []string
	ScriptSrc      []string
	StyleSrc       []string
	ImgSrc         []string
	ConnectSrc     []string
	FontSrc        []string
	ObjectSrc      []string
	FrameAncestors []string
	BaseURI        []string
	FormAction     []string
	ReportURI      string
}

// render serializes the policy; nonce is appended to script-src/style-src
// when non-empty.
func (p Policy) render(nonce string) string {
	var b strings.Builder
	dir := func(name string, srcs []string, withNonce bool) {
		if len(srcs) == 0 && !(withNonce && nonce != "") {
			return
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		for _, s := range srcs {
			b.WriteByte(' ')
			b.WriteString(s)
		}
		if withNonce && nonce != "" {
			b.WriteString(" 'nonce-")
			b.WriteString(nonce)
			b.WriteString("'")
		}
	}
	dir("default-src", p.DefaultSrc, false)
	dir("script-src", p.ScriptSrc, true)
	dir("style-src", p.StyleSrc, true)
	dir("img-src", p.ImgSrc, false)
	dir("connect-src", p.ConnectSrc, false)
	dir("font-src", p.FontSrc, false)
	dir("object-src", p.ObjectSrc, false)
	dir("frame-ancestors", p.FrameAncestors, false)
	dir("base-uri", p.BaseURI, false)
	dir("form-action", p.FormAction, false)
	if p.ReportURI != "" {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString("report-uri ")
		b.WriteString(p.ReportURI)
	}
	return b.String()
}
```

`web/secheaders/options.go`:

```go
package secheaders

type config struct {
	cfg    Config
	policy *Policy
	nonce  bool
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it before other options so
// they can override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithCSP enables the Content-Security-Policy header with the given policy.
func WithCSP(p Policy) Option { return func(cf *config) { cf.policy = &p } }

// WithNonce generates a per-request CSP nonce, appends it to script-src and
// style-src, and exposes it via Nonce(ctx).
func WithNonce() Option { return func(cf *config) { cf.nonce = true } }
```

`web/secheaders/secheaders.go`:

```go
package secheaders

import (
	"context"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/web/middleware"
)

var nonceKey = ctxkey.New[string]("csp_nonce")

// nonceBytes is the entropy of a CSP nonce (base64url-encoded per request).
const nonceBytes = 16

// New returns middleware that sets security headers on every response.
// Headers already set by earlier middleware are left alone, and handlers can
// overwrite anything later — handler wins.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	frameOptions := cf.cfg.FrameOptions
	if frameOptions == "" {
		frameOptions = "DENY"
	}
	hsts := ""
	if cf.cfg.HSTSMaxAge > 0 {
		hsts = "max-age=" + strconv.FormatInt(int64(cf.cfg.HSTSMaxAge.Seconds()), 10)
		if cf.cfg.HSTSIncludeSubdomains {
			hsts += "; includeSubDomains"
		}
	}
	if cf.policy != nil && cf.cfg.CSPReportURI != "" && cf.policy.ReportURI == "" {
		cf.policy.ReportURI = cf.cfg.CSPReportURI
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			setIfEmpty(h, "X-Content-Type-Options", "nosniff")
			setIfEmpty(h, "Referrer-Policy", "strict-origin-when-cross-origin")
			setIfEmpty(h, "Cross-Origin-Opener-Policy", "same-origin")
			if frameOptions != "off" {
				setIfEmpty(h, "X-Frame-Options", frameOptions)
			}
			if hsts != "" {
				setIfEmpty(h, "Strict-Transport-Security", hsts)
			}
			nonce := ""
			if cf.nonce {
				nonce = random.URLSafe(nonceBytes)
				r = r.WithContext(nonceKey.With(r.Context(), nonce))
			}
			if cf.policy != nil {
				setIfEmpty(h, "Content-Security-Policy", cf.policy.render(nonce))
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// Nonce returns the per-request CSP nonce for template use ("" when nonce
// generation is disabled or outside the middleware).
func Nonce(ctx context.Context) string {
	v, _ := nonceKey.From(ctx)
	return v
}

func setIfEmpty(h http.Header, key, value string) {
	if h.Get(key) == "" {
		h.Set(key, value)
	}
}
```

`web/secheaders/doc.go`: package comment explaining default headers, why CSP
is opt-in, the templ nonce recipe (`<script nonce={ secheaders.Nonce(ctx) }>`),
plus a runnable example.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./web/secheaders/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/secheaders/...
just lint
git add web/secheaders
git commit -m "feat(web/secheaders): security headers with typed CSP builder and per-request nonce"
```

---

### Task 5: web/cors — preflight + actual-request CORS

**Files:**
- Create: `web/cors/doc.go`
- Create: `web/cors/config.go`
- Create: `web/cors/options.go`
- Create: `web/cors/errors.go`
- Create: `web/cors/cors.go`
- Test: `web/cors/cors_test.go`

**Interfaces:**
- Consumes: `middleware.Middleware`.
- Produces:
  - `func New(opts ...Option) (middleware.Middleware, error)`
  - `type Config` (prefix `CORS_`), `DefaultConfig()`, `(Config).Validate() error`
  - Options: `WithConfig(Config)`, `WithOriginFunc(func(origin string) bool)`
  - Sentinel: `ErrInvalidConfig`

- [ ] **Step 1: Write the failing tests**

`web/cors/cors_test.go`:

```go
package cors_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/cors"
	"github.com/dmitrymomot/forge/web/middleware"
)

func newMW(t *testing.T, cfg cors.Config, opts ...cors.Option) http.Handler {
	t.Helper()
	mw, err := cors.New(append([]cors.Option{cors.WithConfig(cfg)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // sentinel: handler reached
	}), mw)
}

func TestNonCORSPassesUntouched(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("non-CORS request must reach handler, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no ACAO without Origin header")
	}
}

func TestActualRequestAllowedOrigin(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}, ExposedHeaders: []string{"X-Total-Count"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("ACAO = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got != "X-Total-Count" {
		t.Fatalf("ACEH = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Fatal("Vary: Origin missing")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatal("actual request must reach handler")
	}
}

func TestDisallowedOriginServedWithoutHeaders(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://evil.example.net")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("disallowed origin still served (browser enforces), got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("ACAO must be absent for disallowed origin")
	}
}

func TestPreflight(t *testing.T) {
	cfg := cors.Config{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowCredentials: true,
		MaxAge:           600000000000, // 10m in ns
	}
	h := newMW(t, cfg)
	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("Access-Control-Request-Method", "PUT")
	r.Header.Set("Access-Control-Request-Headers", "Content-Type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
		hd.Get("Access-Control-Allow-Credentials") != "true" ||
		!strings.Contains(hd.Get("Access-Control-Allow-Methods"), "PUT") ||
		hd.Get("Access-Control-Allow-Headers") != "Content-Type" ||
		hd.Get("Access-Control-Max-Age") != "600" {
		t.Fatalf("preflight headers wrong: %+v", hd)
	}
	vary := strings.Join(hd.Values("Vary"), ", ")
	for _, want := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !strings.Contains(vary, want) {
			t.Fatalf("Vary missing %s: %q", want, vary)
		}
	}
}

func TestPreflightDisallowedOriginNoHeaders(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodOptions, "/", nil)
	r.Header.Set("Origin", "https://evil.example.net")
	r.Header.Set("Access-Control-Request-Method", "PUT")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight always terminates, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("no CORS headers for disallowed preflight")
	}
}

func TestWildcardSubdomain(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://*.example.com"}})
	tests := []struct {
		origin string
		want   bool
	}{
		{"https://a.example.com", true},
		{"https://a.b.example.com", false}, // single label only
		{"https://example.com", false},     // base itself not covered
		{"http://a.example.com", false},    // scheme must match
		{"https://aexample.com", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Origin", tt.origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		got := rec.Header().Get("Access-Control-Allow-Origin") != ""
		if got != tt.want {
			t.Errorf("origin %s allowed=%v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestBareWildcard(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"*"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://anywhere.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO = %q, want *", got)
	}
}

func TestWildcardWithCredentialsRejected(t *testing.T) {
	_, err := cors.New(cors.WithConfig(cors.Config{AllowedOrigins: []string{"*"}, AllowCredentials: true}))
	if !errors.Is(err, cors.ErrInvalidConfig) {
		t.Fatalf("bare * + credentials must be rejected, got %v", err)
	}
}

func TestOriginFuncOverrides(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://never.example"}},
		cors.WithOriginFunc(func(origin string) bool { return strings.HasSuffix(origin, ".tenant.example") }))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://acme.tenant.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://acme.tenant.example" {
		t.Fatal("origin func must decide allowance")
	}
}

func TestCredentialsEchoOriginNeverStar(t *testing.T) {
	h := newMW(t, cors.Config{AllowedOrigins: []string{"https://app.example.com"}, AllowCredentials: true})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("with credentials ACAO must echo origin, got %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("ACAC missing")
	}
}

func TestInvalidPatternRejected(t *testing.T) {
	for _, bad := range []string{"https://a.*.example.com", "*.example.com", "https://*", "https://*.", "ftp//x"} {
		if _, err := cors.New(cors.WithConfig(cors.Config{AllowedOrigins: []string{bad}})); !errors.Is(err, cors.ErrInvalidConfig) {
			t.Errorf("pattern %q must be rejected, got %v", bad, err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/cors/...`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement the package**

`web/cors/errors.go`:

```go
package cors

import "errors"

// ErrInvalidConfig marks invalid CORS policy: malformed origin patterns or
// the wildcard-origin + credentials combination.
var ErrInvalidConfig = errors.New("cors: invalid config")
```

`web/cors/config.go`:

```go
package cors

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Config is the env-loadable CORS policy.
type Config struct {
	AllowedOrigins   []string      `env:"CORS_ALLOWED_ORIGINS"` // exact origin, "*", or "https://*.example.com"
	AllowedMethods   []string      `env:"CORS_ALLOWED_METHODS"`
	AllowedHeaders   []string      `env:"CORS_ALLOWED_HEADERS"` // empty = echo the preflight request headers
	ExposedHeaders   []string      `env:"CORS_EXPOSED_HEADERS"`
	AllowCredentials bool          `env:"CORS_ALLOW_CREDENTIALS"`
	MaxAge           time.Duration `env:"CORS_MAX_AGE"` // preflight cache lifetime
}

// DefaultConfig allows the common simple methods with no origins — CORS is
// effectively off until origins are configured.
func DefaultConfig() Config {
	return Config{
		AllowedMethods: []string{
			http.MethodGet, http.MethodPost, http.MethodPut,
			http.MethodPatch, http.MethodDelete, http.MethodHead,
		},
	}
}

// Validate rejects the bare-wildcard + credentials vulnerability and
// malformed origin patterns.
func (c Config) Validate() error {
	for _, o := range c.AllowedOrigins {
		if o == "*" {
			if c.AllowCredentials {
				return fmt.Errorf("%w: wildcard origin with credentials", ErrInvalidConfig)
			}
			continue
		}
		if _, err := parseOrigin(o); err != nil {
			return err
		}
	}
	if c.MaxAge < 0 {
		return fmt.Errorf("%w: negative MaxAge", ErrInvalidConfig)
	}
	return nil
}

// originRule is one compiled AllowedOrigins entry.
type originRule struct {
	exact  string // non-empty for exact matches
	scheme string // for wildcard rules
	base   string // for wildcard rules: suffix after "*."
}

func parseOrigin(o string) (originRule, error) {
	scheme, host, ok := strings.Cut(o, "://")
	if !ok || scheme == "" || host == "" || strings.ContainsAny(host, "/ ") {
		return originRule{}, fmt.Errorf("%w: origin %q must be scheme://host[:port]", ErrInvalidConfig, o)
	}
	if !strings.Contains(o, "*") {
		return originRule{exact: o}, nil
	}
	base, isWildcard := strings.CutPrefix(host, "*.")
	if !isWildcard || base == "" || strings.Contains(base, "*") {
		return originRule{}, fmt.Errorf("%w: wildcard origin %q must be scheme://*.domain", ErrInvalidConfig, o)
	}
	return originRule{scheme: scheme, base: base}, nil
}

// match reports whether origin satisfies the rule. Wildcards cover exactly
// one label: https://*.example.com matches https://a.example.com only.
func (r originRule) match(origin string) bool {
	if r.exact != "" {
		return origin == r.exact
	}
	scheme, host, ok := strings.Cut(origin, "://")
	if !ok || scheme != r.scheme {
		return false
	}
	label, base, ok := strings.Cut(host, ".")
	return ok && label != "" && base == r.base
}
```

`web/cors/options.go`:

```go
package cors

type config struct {
	cfg      Config
	originFn func(origin string) bool
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it first; later options
// override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithOriginFunc replaces AllowedOrigins matching entirely — use it for
// dynamic origins such as DB-backed tenant custom domains.
func WithOriginFunc(fn func(origin string) bool) Option {
	return func(cf *config) { cf.originFn = fn }
}
```

`web/cors/cors.go`:

```go
package cors

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmitrymomot/forge/web/middleware"
)

// New returns CORS middleware. Requests without an Origin header pass
// untouched. Preflights are answered directly with 204; CORS headers are
// emitted only for allowed origins — disallowed requests are still served
// (the browser enforces the policy), matching CORS semantics.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	allowAll := false
	var rules []originRule
	for _, o := range cf.cfg.AllowedOrigins {
		if o == "*" {
			allowAll = true
			continue
		}
		rule, err := parseOrigin(o)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	allowed := func(origin string) bool {
		if cf.originFn != nil {
			return cf.originFn(origin)
		}
		if allowAll {
			return true
		}
		for _, r := range rules {
			if r.match(origin) {
				return true
			}
		}
		return false
	}
	methods := strings.Join(cf.cfg.AllowedMethods, ", ")
	headers := strings.Join(cf.cfg.AllowedHeaders, ", ")
	exposed := strings.Join(cf.cfg.ExposedHeaders, ", ")
	maxAge := ""
	if cf.cfg.MaxAge > 0 {
		maxAge = strconv.FormatInt(int64(cf.cfg.MaxAge.Seconds()), 10)
	}
	credentials := cf.cfg.AllowCredentials
	// ACAO "*" is only valid without credentials and only for the bare
	// wildcard rule; every other allowance echoes the origin.
	star := allowAll && !credentials && cf.originFn == nil

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				if allowed(origin) {
					setOrigin(h, origin, star)
					if credentials {
						h.Set("Access-Control-Allow-Credentials", "true")
					}
					h.Set("Access-Control-Allow-Methods", methods)
					switch {
					case headers != "":
						h.Set("Access-Control-Allow-Headers", headers)
					default:
						if rh := r.Header.Get("Access-Control-Request-Headers"); rh != "" {
							h.Set("Access-Control-Allow-Headers", rh)
						}
					}
					if maxAge != "" {
						h.Set("Access-Control-Max-Age", maxAge)
					}
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if allowed(origin) {
				setOrigin(h, origin, star)
				if credentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				if exposed != "" {
					h.Set("Access-Control-Expose-Headers", exposed)
				}
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func setOrigin(h http.Header, origin string, star bool) {
	if star {
		h.Set("Access-Control-Allow-Origin", "*")
		return
	}
	h.Set("Access-Control-Allow-Origin", origin)
}
```

`web/cors/doc.go`: package comment with an env-loaded example
(`CORS_ALLOWED_ORIGINS=https://app.example.com,https://*.tenant.example.com`)
and a `WithOriginFunc` tenant-domains example.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./web/cors/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/cors/...
just lint
git add web/cors
git commit -m "feat(web/cors): env-loadable CORS policy with wildcard subdomains and dynamic origin func"
```

---

### Task 6: web/timeout — per-request deadline middleware

**Files:**
- Create: `web/timeout/doc.go`
- Create: `web/timeout/config.go`
- Create: `web/timeout/options.go`
- Create: `web/timeout/errors.go`
- Create: `web/timeout/timeout.go`
- Test: `web/timeout/timeout_test.go`

**Interfaces:**
- Consumes: `middleware.Middleware`, `middleware.WrapWriter`, `problem.JSON/WithStatus`.
- Produces:
  - `func New(opts ...Option) (middleware.Middleware, error)`
  - `type Config { Timeout time.Duration }` (env `TIMEOUT_DURATION`), `DefaultConfig()` (30s), `Validate()`
  - Options: `WithConfig(Config)`, `WithResponder(problem.Responder)`
  - Sentinels: `ErrTimeout`, `ErrInvalidConfig`

- [ ] **Step 1: Write the failing tests**

`web/timeout/timeout_test.go`:

```go
package timeout_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/timeout"
)

func newMW(t *testing.T, d time.Duration) middleware.Middleware {
	t.Helper()
	mw, err := timeout.New(timeout.WithConfig(timeout.Config{Timeout: d}))
	if err != nil {
		t.Fatal(err)
	}
	return mw
}

func TestSlowHandlerGets504(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // ctx-respecting slow handler
	}), newMW(t, 10*time.Millisecond))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("want problem+json, got %q", ct)
	}
}

func TestFastHandlerUntouched(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}), newMW(t, time.Second))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("fast handler mangled: %d %q", rec.Code, rec.Body.String())
	}
}

func TestDeadlinePropagatedToContext(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("deadline missing from request context")
		}
	}), newMW(t, time.Second))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestAlreadyWrittenResponseLeftAlone(t *testing.T) {
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, "partial")
		<-r.Context().Done()
	}), newMW(t, 10*time.Millisecond))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusAccepted || rec.Body.String() != "partial" {
		t.Fatalf("committed response must not be rewritten: %d %q", rec.Code, rec.Body.String())
	}
}

func TestInvalidConfig(t *testing.T) {
	if _, err := timeout.New(timeout.WithConfig(timeout.Config{Timeout: 0})); !errors.Is(err, timeout.ErrInvalidConfig) {
		t.Fatalf("zero timeout must be rejected, got %v", err)
	}
}

func TestDefaultConfig(t *testing.T) {
	if d := timeout.DefaultConfig().Timeout; d != 30*time.Second {
		t.Fatalf("default = %v, want 30s", d)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/timeout/...`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement the package**

`web/timeout/errors.go`:

```go
package timeout

import "errors"

var (
	// ErrTimeout is passed to the responder when the request deadline expires
	// before the handler writes a response.
	ErrTimeout = errors.New("timeout: request deadline exceeded")
	// ErrInvalidConfig marks a non-positive Timeout.
	ErrInvalidConfig = errors.New("timeout: invalid config")
)
```

`web/timeout/config.go`:

```go
package timeout

import (
	"fmt"
	"time"
)

// Config is the env-loadable deadline policy.
type Config struct {
	Timeout time.Duration `env:"TIMEOUT_DURATION"`
}

// DefaultConfig returns a 30-second request deadline.
func DefaultConfig() Config { return Config{Timeout: 30 * time.Second} }

// Validate requires a positive Timeout.
func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return fmt.Errorf("%w: Timeout must be > 0, got %v", ErrInvalidConfig, c.Timeout)
	}
	return nil
}
```

`web/timeout/options.go`:

```go
package timeout

import "github.com/dmitrymomot/forge/web/problem"

type config struct {
	cfg       Config
	responder problem.Responder
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it first; later options
// override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithResponder overrides the 504 response (default problem.JSON).
func WithResponder(r problem.Responder) Option {
	return func(cf *config) {
		if r != nil {
			cf.responder = r
		}
	}
}
```

`web/timeout/timeout.go`:

```go
package timeout

import (
	"context"
	"errors"
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/problem"
)

// New returns middleware that puts a deadline on every request context and
// answers 504 when a ctx-respecting handler returns without writing after
// the deadline expired. Enforcement is cooperative — handlers that ignore
// their context keep running; the deadline reaches them via r.Context().
//
// Do not wrap streaming routes (SSE, long-poll): compose with
// middleware.Skip to exempt them.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{
		cfg:       DefaultConfig(),
		responder: problem.JSON(problem.WithStatus(http.StatusGatewayTimeout)),
	}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	d := cf.cfg.Timeout
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			rw := middleware.WrapWriter(w)
			next.ServeHTTP(rw, r.WithContext(ctx))
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && !rw.Wrote() {
				cf.responder(rw, r, ErrTimeout)
			}
		})
	}, nil
}
```

`web/timeout/doc.go`: package comment + runnable example including the
`middleware.Skip(sse-route predicate, mw)` streaming exemption recipe.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./web/timeout/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/timeout/...
just lint
git add web/timeout
git commit -m "feat(web/timeout): cooperative per-request deadline middleware with 504 problem response"
```

---

### Task 7: web/compress — gzip/deflate response compression

**Files:**
- Create: `web/compress/doc.go`
- Create: `web/compress/config.go`
- Create: `web/compress/options.go`
- Create: `web/compress/accept.go`
- Create: `web/compress/writer.go`
- Create: `web/compress/compress.go`
- Test: `web/compress/compress_test.go`

**Interfaces:**
- Consumes: `middleware.Middleware`.
- Produces:
  - `func New(opts ...Option) (middleware.Middleware, error)`
  - `type Config { MinSize, Level int }` (env `COMPRESS_MIN_SIZE`, `COMPRESS_LEVEL`), `DefaultConfig()` (1024, gzip.DefaultCompression), `Validate()`
  - Options: `WithConfig(Config)`, `WithContentTypes(types ...string)`
  - Sentinel: `ErrInvalidConfig`

- [ ] **Step 1: Write the failing tests**

`web/compress/compress_test.go`:

```go
package compress_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/compress"
	"github.com/dmitrymomot/forge/web/middleware"
)

func newHandler(t *testing.T, h http.HandlerFunc, opts ...compress.Option) http.Handler {
	t.Helper()
	mw, err := compress.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return middleware.Wrap(h, mw)
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func bigBody() string { return strings.Repeat("forge compresses text. ", 200) } // ~4.6 KB

func TestGzipLargeTextResponse(t *testing.T) {
	body := bigBody()
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, body)
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip, deflate")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatal("Content-Length must be stripped")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatal("Vary: Accept-Encoding missing")
	}
	if got := gunzip(t, rec.Body.Bytes()); got != body {
		t.Fatal("round-trip mismatch")
	}
}

func TestSmallResponseSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "tiny")
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("sub-MinSize body must not be compressed")
	}
	if rec.Body.String() != "tiny" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestNoAcceptEncodingPassthrough(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, bigBody())
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("no Accept-Encoding → no compression")
	}
}

func TestQZeroDisablesGzip(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip;q=0, deflate;q=0")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("q=0 must disable compression")
	}
}

func TestGzipPreferredOverDeflateOnTie(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "deflate, gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("tie must prefer gzip, got %q", got)
	}
}

func TestDisallowedContentTypeSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(bytes.Repeat([]byte{0x89}, 4096))
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("image/png must not be compressed")
	}
}

func TestAlreadyEncodedSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "br")
		io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("pre-encoded response must pass through, got %q", got)
	}
}

func TestRangeRequestSkipped(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, bigBody())
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	r.Header.Set("Range", "bytes=0-99")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("Range requests must not be compressed")
	}
}

func TestStatusCodePreserved(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, strings.Repeat(`{"k":"v"}`, 600))
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("compressed 201 expected")
	}
}

func TestFlushSupportsSSE(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		io.WriteString(w, "data: two\n\n")
		w.(http.Flusher).Flush()
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("SSE with gzip client should compress")
	}
	if !rec.Flushed {
		t.Fatal("Flush must reach the underlying writer")
	}
	if got := gunzip(t, rec.Body.Bytes()); got != "data: one\n\ndata: two\n\n" {
		t.Fatalf("SSE payload mangled: %q", got)
	}
}

func TestEmptyBodyNoPanic(t *testing.T) {
	h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("204 mangled: %d %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
}

func TestInvalidLevelRejected(t *testing.T) {
	if _, err := compress.New(compress.WithConfig(compress.Config{MinSize: 1024, Level: 42})); !errors.Is(err, compress.ErrInvalidConfig) {
		t.Fatalf("level 42 must be rejected, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./web/compress/...`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement the package**

`web/compress/config.go` (includes errors — the package has a single sentinel):

```go
package compress

import (
	"compress/gzip"
	"errors"
	"fmt"
)

// ErrInvalidConfig marks an out-of-range Level or negative MinSize.
var ErrInvalidConfig = errors.New("compress: invalid config")

// Config is the env-loadable compression policy.
type Config struct {
	MinSize int `env:"COMPRESS_MIN_SIZE"` // bytes buffered before compressing kicks in
	Level   int `env:"COMPRESS_LEVEL"`    // gzip/flate level
}

// DefaultConfig returns MinSize=1024 and the default compression level.
func DefaultConfig() Config {
	return Config{MinSize: 1024, Level: gzip.DefaultCompression}
}

// Validate checks Level against the gzip/flate range and MinSize >= 0.
func (c Config) Validate() error {
	if c.Level != gzip.DefaultCompression && (c.Level < gzip.HuffmanOnly || c.Level > gzip.BestCompression) {
		return fmt.Errorf("%w: level %d", ErrInvalidConfig, c.Level)
	}
	if c.MinSize < 0 {
		return fmt.Errorf("%w: negative MinSize", ErrInvalidConfig)
	}
	return nil
}
```

`web/compress/accept.go`:

```go
package compress

import (
	"strconv"
	"strings"
)

// negotiate picks gzip or deflate from an Accept-Encoding header, preferring
// gzip on equal q-values. It returns "" when neither is acceptable.
func negotiate(header string) string {
	if header == "" {
		return ""
	}
	qGzip, qDeflate := -1.0, -1.0
	for part := range strings.SplitSeq(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		q := 1.0
		if qs, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if v, err := strconv.ParseFloat(strings.TrimSpace(qs), 64); err == nil {
				q = v
			}
		}
		switch name {
		case "gzip", "x-gzip":
			qGzip = q
		case "deflate":
			qDeflate = q
		}
	}
	switch {
	case qGzip > 0 && qGzip >= qDeflate:
		return "gzip"
	case qDeflate > 0:
		return "deflate"
	default:
		return ""
	}
}
```

`web/compress/writer.go`:

```go
package compress

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// zwriter is the common surface of *gzip.Writer and *flate.Writer.
type zwriter interface {
	io.Writer
	Flush() error
	Close() error
	Reset(io.Writer)
}

// writer buffers up to minSize bytes before deciding whether to compress,
// so headers (Content-Encoding, Content-Length) and the status line go out
// correct. Flush forces an immediate decision — SSE streams compress from
// the first event.
type writer struct {
	rw      http.ResponseWriter
	rc      *http.ResponseController
	pool    *sync.Pool
	enc     string
	types   []string
	buf     []byte
	minSize int
	status  int
	zw      zwriter
	decided bool
}

func (w *writer) Header() http.Header { return w.rw.Header() }

func (w *writer) WriteHeader(code int) {
	if w.decided {
		return
	}
	if w.status == 0 {
		w.status = code
	}
}

func (w *writer) Write(p []byte) (int, error) {
	if !w.decided {
		w.buf = append(w.buf, p...)
		if len(w.buf) >= w.minSize {
			if err := w.decide(false); err != nil {
				return 0, err
			}
		}
		return len(p), nil
	}
	if w.zw != nil {
		return w.zw.Write(p)
	}
	return w.rw.Write(p)
}

// Flush decides immediately (streaming) and flushes through to the client.
func (w *writer) Flush() {
	_ = w.decide(false)
	if w.zw != nil {
		_ = w.zw.Flush()
	}
	_ = w.rc.Flush()
}

// Unwrap exposes the underlying writer for http.ResponseController.
func (w *writer) Unwrap() http.ResponseWriter { return w.rw }

// decide picks compressed vs plain, sends the delayed status + buffer, and
// locks the choice in. final=true means the body is complete (close path).
func (w *writer) decide(final bool) error {
	if w.decided {
		return nil
	}
	w.decided = true
	h := w.rw.Header()
	ct := h.Get("Content-Type")
	if ct == "" && len(w.buf) > 0 {
		ct = http.DetectContentType(w.buf)
		h.Set("Content-Type", ct)
	}
	compressing := h.Get("Content-Encoding") == "" &&
		typeAllowed(w.types, ct) &&
		!(final && len(w.buf) < w.minSize)
	if compressing {
		h.Set("Content-Encoding", w.enc)
		h.Del("Content-Length")
		zw := w.pool.Get().(zwriter)
		zw.Reset(w.rw)
		w.zw = zw
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.rw.WriteHeader(w.status)
	if len(w.buf) > 0 {
		var err error
		if w.zw != nil {
			_, err = w.zw.Write(w.buf)
		} else {
			_, err = w.rw.Write(w.buf)
		}
		w.buf = nil
		return err
	}
	return nil
}

// close finalizes the response: undecided buffers flush as-is (plain when
// under MinSize), compressed streams close and return their writer to the
// pool.
func (w *writer) close() {
	_ = w.decide(true)
	if w.zw != nil {
		_ = w.zw.Close()
		w.pool.Put(w.zw)
		w.zw = nil
	}
}

func typeAllowed(allowed []string, ct string) bool {
	ct, _, _ = strings.Cut(ct, ";")
	ct = strings.TrimSpace(strings.ToLower(ct))
	for _, a := range allowed {
		if base, ok := strings.CutSuffix(a, "/*"); ok {
			if strings.HasPrefix(ct, base+"/") {
				return true
			}
			continue
		}
		if ct == a {
			return true
		}
	}
	return false
}

func newPool(enc string, level int) *sync.Pool {
	return &sync.Pool{New: func() any {
		if enc == "deflate" {
			zw, _ := flate.NewWriter(io.Discard, level)
			return zwriter(zw)
		}
		zw, _ := gzip.NewWriterLevel(io.Discard, level)
		return zwriter(zw)
	}}
}
```

`web/compress/options.go`:

```go
package compress

type config struct {
	cfg   Config
	types []string
}

// Option configures the middleware.
type Option func(*config)

// WithConfig applies an env-loaded Config. Place it first; later options
// override.
func WithConfig(c Config) Option { return func(cf *config) { cf.cfg = c } }

// WithContentTypes replaces the compressible content-type allowlist.
// Entries are exact ("application/json") or family wildcards ("text/*").
func WithContentTypes(types ...string) Option { return func(cf *config) { cf.types = types } }
```

`web/compress/compress.go`:

```go
package compress

import (
	"net/http"
	"sync"

	"github.com/dmitrymomot/forge/web/middleware"
)

// defaultTypes are the content types worth compressing. Binary formats
// (images, video, archives) are already compressed and excluded.
var defaultTypes = []string{"text/*", "application/json", "application/javascript", "image/svg+xml"}

// New returns response-compression middleware negotiating gzip/deflate from
// Accept-Encoding. Responses under MinSize, non-matching content types,
// Range requests, HEAD requests, upgrades, and pre-encoded responses pass
// through unchanged. Flusher is preserved: each Flush drains the compressor
// so SSE frames reach the client immediately.
func New(opts ...Option) (middleware.Middleware, error) {
	cf := config{cfg: DefaultConfig(), types: defaultTypes}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	pools := map[string]*sync.Pool{
		"gzip":    newPool("gzip", cf.cfg.Level),
		"deflate": newPool("deflate", cf.cfg.Level),
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Accept-Encoding")
			enc := negotiate(r.Header.Get("Accept-Encoding"))
			if enc == "" || r.Method == http.MethodHead ||
				r.Header.Get("Range") != "" || r.Header.Get("Upgrade") != "" {
				next.ServeHTTP(w, r)
				return
			}
			cw := &writer{
				rw:      w,
				rc:      http.NewResponseController(w),
				pool:    pools[enc],
				enc:     enc,
				types:   cf.types,
				minSize: cf.cfg.MinSize,
			}
			defer cw.close()
			next.ServeHTTP(cw, r)
		})
	}, nil
}
```

`web/compress/doc.go`: package comment covering negotiation, MinSize
buffering, the SSE Flush guarantee, ordering advice (compress goes outside
handlers, inside reqlog), and a runnable example.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./web/compress/...`
Expected: PASS.

- [ ] **Step 5: Format, lint, commit**

```bash
just fmt ./web/compress/...
just lint
git add web/compress
git commit -m "feat(web/compress): gzip/deflate negotiation with MinSize buffering and SSE-safe flushing"
```

---

### Task 8: docs/packages.md update + repo-wide verification

**Files:**
- Modify: `docs/packages.md`

**Interfaces:**
- Consumes: everything shipped in Tasks 1–7.
- Produces: the roadmap doc reflecting the new state.

- [ ] **Step 1: Update docs/packages.md**

Apply exactly:

1. Header state line: `**State: 61 shipped packages**` → `**State: 67 shipped packages**`; `**60 roadmap**` → `**54 roadmap**`.
2. Target tree, `web/` block: move `cookie csrf secheaders cors timeout compress` from the `# planned:` line to the `# shipped:` line.
3. `### web/` catalog section: extend the `Shipped:` sentence with the six names + a one-line parenthetical for the codec (mirroring the httpclient precedent), and delete the six planned bullet entries (`**cookie**`, `**csrf**`, `**secheaders**`, `**cors**`, `**timeout**`, `**compress**`).
4. Build order wave 1: remove the shipped six from the wave-1 list (keep `assets`, `iplist`, `webhookverify`, `autocert`, `captcha`, `idempotency` and the ops/testkit entries).
5. Add the env-prefix convention to "Design DNA every package follows" as a new bullet:
   ```
   - **Env prefixes are baked into tags:** every env-loadable `Config` carries
     its package prefix in the tag (`COOKIE_KEYS`, `SERVER_ADDR`, `DB_URL`).
     Nest untagged to keep default names; nest tagged (`env:"APP"`) to
     separate instances (`APP_COOKIE_KEYS`).
   ```

- [ ] **Step 2: Full-tree verification**

Run:
```bash
go build ./... && go test -race ./... && just lint
```
Expected: all green.

- [ ] **Step 3: Commit**

```bash
git add docs/packages.md
git commit -m "docs(packages): mark web-boundary security bundle shipped; document env-prefix convention"
```

---

## Post-plan (not tasks): PR flow

Per CLAUDE.md: push branch, open PR against `main`, wait for CI, fix failures, read Claude's review, fix all issues, resolve threads, repeat until clean. The claude-code-review workflow can time out on large PRs and still "pass" — run an explicit review if it posts nothing.
