# Wave 1 Completion Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete build-order Wave 1 by shipping `ops/config`, `ops/health`, `resilience/ratelimit` (+`ratelimit/redisstore`), and `web/httpclient`, plus two shared unblockers (`core/structfields.SetString`, `ops/supervisor.WithPreShutdown`), as one PR.

**Architecture:** Each package follows forge's design DNA: free-funcs or `New(...Option)` (never builders), `errors.Is`-matchable single-line sentinels, `doc.go` runnable example, black-box tests only. `config` layers YAML (per-env + `${VAR:default}` substitution) over `.env` inheritance and env-tag structs. `health` is a single pull handler factory. `ratelimit` establishes the counter `Store` seam (sliding-window). `httpclient` composes the shipped `retry`/`backoff`/`circuitbreaker` into a RoundTripper stack.

**Tech Stack:** Go 1.26, stdlib, `gopkg.in/yaml.v3` (new — `config` only), `github.com/redis/go-redis/v9` (`redisstore` only), existing forge packages (`core/structfields`, `core/typeconv`, `core/clock`, `web/middleware`, `web/problem`, `resilience/{retry,backoff,circuitbreaker}`, `ops/supervisor`).

## Global Constraints

- Work ONLY in the current branch (`claude/heuristic-kepler-922e58`); never switch branches.
- Module path: `github.com/dmitrymomot/forge`. Single `go.mod`, Go 1.26.
- **Black-box tests only** — test files use `package <pkg>_test`.
- Packages: max two levels (`domain/package`); a third level only for driver isolators (`ratelimit/redisstore`). Leaf directory = package name, unique across domains.
- No builder pattern — use `type Option func(*config)` functional options.
- Minimal dependencies: only `gopkg.in/yaml.v3` is added, used solely by `ops/config`.
- Single-line, `errors.Is`-matchable sentinels in `errors.go`. Public methods never return unexported types.
- After file changes run `just fmt ./<domain>/<pkg>/...` (package-path form — the single-file form trips a spurious betteralign "undefined"). Run `just lint` when the task is done. Run `modernize` (`go tool modernize ./...`) before finishing; prefer `errors.AsType` over `errors.As` where modernize suggests it (matches shipped code).
- Test command: `just test ./<domain>/<pkg>/...` (runs `go test -race -cover`).
- No Claude attribution in commits.

---

## Task 1: `core/structfields.SetString` (unblocker)

Parses a string into a struct field by kind and assigns it via the existing `Field.Set`. Consumed by `ops/config`'s env-tag path.

**Files:**
- Create: `core/structfields/setstring.go`
- Modify: `core/structfields/errors.go` (add `ErrUnsupportedKind`)
- Test: `core/structfields/setstring_test.go`

**Interfaces:**
- Consumes: `structfields.Field` (`.Value reflect.Value`, `.Set func(any) error`, `.Name string`), `typeconv.{ParseBool,ParseInt,ParseUint,ParseFloat,ParseDuration,ParseTime,ParseSlice}`.
- Produces: `func SetString(f Field, raw string) error`; `var ErrUnsupportedKind error`.

- [ ] **Step 1: Write the failing test**

```go
package structfields_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/structfields"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetString_SupportedKinds(t *testing.T) {
	var s struct {
		Name    string
		Count   int
		Small   int32
		Size    uint16
		Ratio   float64
		On      bool
		Timeout time.Duration
		At      time.Time
		Origins []string
	}
	apply := func(name, raw string) {
		require.NoError(t, structfields.Walk(&s, "env", func(f structfields.Field) error {
			if f.Name == name {
				return structfields.SetString(f, raw)
			}
			return nil
		}))
	}
	apply("Name", "forge")
	apply("Count", "42")
	apply("Small", "7")
	apply("Size", "9")
	apply("Ratio", "3.5")
	apply("On", "true")
	apply("Timeout", "1500ms")
	apply("At", "2026-07-05T10:00:00Z")
	apply("Origins", "a,b,c")

	assert.Equal(t, "forge", s.Name)
	assert.Equal(t, 42, s.Count)
	assert.Equal(t, int32(7), s.Small)
	assert.Equal(t, uint16(9), s.Size)
	assert.InDelta(t, 3.5, s.Ratio, 1e-9)
	assert.True(t, s.On)
	assert.Equal(t, 1500*time.Millisecond, s.Timeout)
	assert.Equal(t, 2026, s.At.Year())
	assert.Equal(t, []string{"a", "b", "c"}, s.Origins)
}

func TestSetString_UnsupportedKind(t *testing.T) {
	var s struct{ M map[string]string }
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		return structfields.SetString(f, "x")
	})
	assert.ErrorIs(t, err, structfields.ErrUnsupportedKind)
}

func TestSetString_BadSyntax(t *testing.T) {
	var s struct{ Count int }
	err := structfields.Walk(&s, "env", func(f structfields.Field) error {
		return structfields.SetString(f, "not-a-number")
	})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/structfields/...`
Expected: FAIL — `SetString`, `ErrUnsupportedKind` undefined.

- [ ] **Step 3: Add the sentinel**

In `core/structfields/errors.go`, append:

```go
// ErrUnsupportedKind is returned by SetString when the field's kind cannot be
// parsed from a string.
var ErrUnsupportedKind = errors.New("structfields: unsupported kind")
```

- [ ] **Step 4: Write the implementation**

Create `core/structfields/setstring.go`:

```go
package structfields

import (
	"fmt"
	"reflect"
	"time"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// SetString parses raw according to f's field type and assigns it via f.Set.
// Supported: string, bool, all int/uint widths, float32/64, time.Duration,
// time.Time (RFC3339), and []string (comma-separated). Other kinds return
// ErrUnsupportedKind. A read-only (value-struct) field returns ErrNotSettable
// through f.Set; a parse failure surfaces typeconv.ErrSyntax.
func SetString(f Field, raw string) error {
	switch f.Value.Type() {
	case reflect.TypeOf(time.Duration(0)):
		d, err := typeconv.ParseDuration(raw)
		if err != nil {
			return err
		}
		return f.Set(d)
	case reflect.TypeOf(time.Time{}):
		tm, err := typeconv.ParseTime(raw)
		if err != nil {
			return err
		}
		return f.Set(tm)
	}

	switch f.Value.Kind() {
	case reflect.String:
		return f.Set(raw)
	case reflect.Bool:
		v, err := typeconv.ParseBool(raw)
		if err != nil {
			return err
		}
		return f.Set(v)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := typeconv.ParseInt[int64](raw)
		if err != nil {
			return err
		}
		return f.Set(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := typeconv.ParseUint[uint64](raw)
		if err != nil {
			return err
		}
		return f.Set(v)
	case reflect.Float32, reflect.Float64:
		v, err := typeconv.ParseFloat[float64](raw)
		if err != nil {
			return err
		}
		return f.Set(v)
	case reflect.Slice:
		if f.Value.Type().Elem().Kind() == reflect.String {
			v, err := typeconv.ParseSlice[string](raw, ",")
			if err != nil {
				return err
			}
			return f.Set(v)
		}
	}
	return fmt.Errorf("structfields: field %q: %w (%s)", f.Name, ErrUnsupportedKind, f.Value.Kind())
}
```

Note: parsing to the widest type (`int64`/`uint64`/`float64`) then `f.Set` relies on `makeSetter`'s `ConvertibleTo` path to narrow to the field's actual width. Only `[]string` slices are supported in v1 (richer element types can be added later — flagged in the spec).

- [ ] **Step 5: Run tests to verify they pass**

Run: `just test ./core/structfields/...`
Expected: PASS.

- [ ] **Step 6: Format, lint, commit**

```bash
just fmt ./core/structfields/...
just lint
git add core/structfields/setstring.go core/structfields/errors.go core/structfields/setstring_test.go
git commit -m "feat(core/structfields): SetString parses a string into a field by kind"
```

---

## Task 2: `ops/supervisor.WithPreShutdown` (unblocker)

Adds an ordered pre-shutdown phase: hooks run to completion (bounded) after the stop signal but BEFORE `runCtx` is cancelled, so `health`'s readiness flip lands before `httpserver` stops accepting.

**Files:**
- Modify: `ops/supervisor/options.go` (add `preHook` type, `WithPreShutdown`, `WithPreShutdownTimeout`, config fields + default)
- Modify: `ops/supervisor/supervisor.go` (run hooks inside `beginShutdown` before `cancel()`)
- Modify: `ops/supervisor/errors.go` (add `ErrPreShutdownTimeout`)
- Test: `ops/supervisor/preshutdown_test.go`

**Interfaces:**
- Consumes: existing `config` struct, `Option`, `beginShutdown` closure in `Run`.
- Produces: `func WithPreShutdown(name string, fn func(context.Context)) Option`; `func WithPreShutdownTimeout(d time.Duration) Option`; `var ErrPreShutdownTimeout error`.

- [ ] **Step 1: Write the failing test**

```go
package supervisor_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/supervisor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The hook must FINISH before the service observes ctx cancellation.
func TestWithPreShutdown_RunsBeforeServiceCancel(t *testing.T) {
	var order []string
	var mu atomic.Int32 // sequence counter

	hookSeq := int32(-1)
	svcSeq := int32(-1)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	err := supervisor.Run(ctx,
		supervisor.WithPreShutdown("drain", func(context.Context) {
			hookSeq = mu.Add(1)
			_ = order
		}),
		supervisor.WithServiceFunc("svc", func(sctx context.Context) error {
			<-sctx.Done()
			svcSeq = mu.Add(1)
			return nil
		}),
		supervisor.WithShutdownTimeout(time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), hookSeq, "hook should run first")
	assert.Equal(t, int32(2), svcSeq, "service cancel should observe after hook")
}

func TestWithPreShutdown_TimeoutSurfaces(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()

	err := supervisor.Run(ctx,
		supervisor.WithPreShutdownTimeout(20*time.Millisecond),
		supervisor.WithPreShutdown("slow", func(hctx context.Context) {
			<-hctx.Done() // never returns before the bound
		}),
		supervisor.WithServiceFunc("svc", func(sctx context.Context) error { <-sctx.Done(); return nil }),
		supervisor.WithShutdownTimeout(time.Second),
	)
	assert.ErrorIs(t, err, supervisor.ErrPreShutdownTimeout)
}

func TestWithPreShutdown_NilFuncRejected(t *testing.T) {
	err := supervisor.Run(context.Background(),
		supervisor.WithPreShutdown("bad", nil),
		supervisor.WithServiceFunc("svc", func(context.Context) error { return nil }),
	)
	assert.ErrorIs(t, err, supervisor.ErrInvalidConfig)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/supervisor/...`
Expected: FAIL — `WithPreShutdown`, `WithPreShutdownTimeout`, `ErrPreShutdownTimeout` undefined.

- [ ] **Step 3: Add the sentinel**

In `ops/supervisor/errors.go`, add to the `var (...)` block:

```go
	// ErrPreShutdownTimeout is returned (joined) by Run when pre-shutdown hooks do not finish within the pre-shutdown timeout.
	ErrPreShutdownTimeout = errors.New("supervisor: pre-shutdown timed out")
```

- [ ] **Step 4: Extend options and config**

In `ops/supervisor/options.go`, add the hook type and fields, and set the default. Change the `config` struct and `defaultConfig`:

```go
// preHook is a named pre-shutdown callback.
type preHook struct {
	name string
	fn   func(context.Context)
}

// (add these fields to the existing `config` struct)
//	preShutdown        []preHook
//	preShutdownTimeout time.Duration
```

Update `defaultConfig()` to seed `preShutdownTimeout: 30 * time.Second`. Then add the options:

```go
// WithPreShutdown registers a hook run after shutdown begins but BEFORE each
// service's context is cancelled — so readiness can flip and load balancers can
// deregister while services still serve. Hooks run concurrently; Run waits for
// them, bounded by WithPreShutdownTimeout. A nil fn is rejected as ErrInvalidConfig.
func WithPreShutdown(name string, fn func(context.Context)) Option {
	return func(c *config) {
		if fn == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithPreShutdown(%q) received a nil func", ErrInvalidConfig, name))
			return
		}
		c.preShutdown = append(c.preShutdown, preHook{name: name, fn: fn})
	}
}

// WithPreShutdownTimeout bounds the pre-shutdown phase. Default 30s; it must
// exceed the longest grace a hook waits internally, or the hook is cut short.
func WithPreShutdownTimeout(d time.Duration) Option {
	return func(c *config) { c.preShutdownTimeout = d }
}
```

- [ ] **Step 5: Run hooks before cancel in `beginShutdown`**

In `ops/supervisor/supervisor.go`, add the runner function and call it from `beginShutdown` before `cancel()`. Add imports `context`, `sync` (both likely already present — `context` is). New function:

```go
// runPreShutdown runs all hooks concurrently, returning ErrPreShutdownTimeout if
// they do not all finish within timeout (0 = wait indefinitely). A panicking hook
// is recovered and logged.
func runPreShutdown(hooks []preHook, timeout time.Duration, log *slog.Logger) error {
	if len(hooks) == 0 {
		return nil
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var wg sync.WaitGroup
	for _, h := range hooks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					log.Error("pre-shutdown hook panicked",
						slog.String("hook", h.name), slog.Any("panic", p))
				}
			}()
			log.Info("pre-shutdown hook started", slog.String("hook", h.name))
			h.fn(ctx)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ErrPreShutdownTimeout
	}
}
```

Then modify `beginShutdown` (currently at `supervisor.go:75`) to run hooks before cancel:

```go
	beginShutdown := func(reason string) {
		if shuttingDown {
			return
		}
		shuttingDown = true
		log.Info("shutdown started", slog.String("reason", reason))
		if err := runPreShutdown(cfg.preShutdown, cfg.preShutdownTimeout, log); err != nil {
			errs = append(errs, err)
		}
		cancel()
		done = nil
		if cfg.ShutdownTimeout > 0 {
			graceCh = time.After(cfg.ShutdownTimeout)
		}
	}
```

(`errs` is the slice already declared above `beginShutdown`; appending inside the closure mutates it.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `just test ./ops/supervisor/...`
Expected: PASS (including the existing supervisor tests — no-hooks path is unchanged).

- [ ] **Step 7: Format, lint, commit**

```bash
just fmt ./ops/supervisor/...
just lint
git add ops/supervisor/
git commit -m "feat(ops/supervisor): WithPreShutdown ordered pre-shutdown phase before service cancel"
```

---

## Task 3: `ops/config` — `${VAR:default}` substitution

**Files:**
- Create: `ops/config/substitute.go`, `ops/config/errors.go`
- Test: `ops/config/substitute_test.go`

**Interfaces:**
- Produces: `func Substitute(s string) (string, error)`; internal `func substitute(s string, lookup func(string) (string, bool)) (string, error)`; sentinels `ErrProfileFile, ErrSubstitute, ErrYAML, ErrDotenv, ErrRequiredMissing, ErrParse`.

- [ ] **Step 1: Write the failing test**

```go
package config_test

import (
	"testing"

	"github.com/dmitrymomot/forge/ops/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubstitute(t *testing.T) {
	t.Setenv("HOST", "10.0.0.1")
	t.Setenv("EMPTY", "")

	got, err := config.Substitute("host: ${HOST:0.0.0.0}")
	require.NoError(t, err)
	assert.Equal(t, "host: 10.0.0.1", got)

	got, err = config.Substitute("host: ${MISSING:0.0.0.0}")
	require.NoError(t, err)
	assert.Equal(t, "host: 0.0.0.0", got)

	got, err = config.Substitute("host: ${EMPTY:fallback}") // empty -> default
	require.NoError(t, err)
	assert.Equal(t, "host: fallback", got)

	got, err = config.Substitute("url: ${MISSING:http://x:8080}") // colon in default
	require.NoError(t, err)
	assert.Equal(t, "url: http://x:8080", got)

	got, err = config.Substitute("price: $$5")
	require.NoError(t, err)
	assert.Equal(t, "price: $5", got)

	_, err = config.Substitute("x: ${NOPE}") // unset, no default -> error
	assert.ErrorIs(t, err, config.ErrSubstitute)

	_, err = config.Substitute("x: ${UNTERMINATED")
	assert.ErrorIs(t, err, config.ErrSubstitute)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/config/...`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Add sentinels**

Create `ops/config/errors.go`:

```go
package config

import "errors"

var (
	// ErrProfileFile is returned by Load when the {profile}.yaml file is missing or unreadable.
	ErrProfileFile = errors.New("config: profile file")
	// ErrSubstitute is returned when a ${VAR} placeholder is malformed or an unset no-default var is referenced.
	ErrSubstitute = errors.New("config: substitution")
	// ErrYAML is returned when the substituted YAML fails to unmarshal.
	ErrYAML = errors.New("config: yaml")
	// ErrDotenv is returned when a .env file cannot be read or applied.
	ErrDotenv = errors.New("config: dotenv")
	// ErrRequiredMissing is returned by LoadEnv/Populate when a required env key has no value.
	ErrRequiredMissing = errors.New("config: required env missing")
	// ErrParse is returned by LoadEnv/Populate when a value cannot be parsed into its field.
	ErrParse = errors.New("config: parse")
)
```

- [ ] **Step 4: Write the substitution implementation**

Create `ops/config/substitute.go`:

```go
package config

import (
	"fmt"
	"os"
	"strings"
)

// Substitute expands ${VAR} and ${VAR:default} placeholders in s against the
// process environment. $$ yields a literal $. ${VAR} with VAR unset is an
// error; ${VAR:default} falls back to default when VAR is unset OR empty. The
// name/default split is on the first colon, so defaults may contain colons.
func Substitute(s string) (string, error) {
	return substitute(s, os.LookupEnv)
}

func substitute(s string, lookup func(string) (string, bool)) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			rel := strings.IndexByte(s[i+2:], '}')
			if rel < 0 {
				return "", fmt.Errorf("%w: unterminated placeholder", ErrSubstitute)
			}
			val, err := resolvePlaceholder(s[i+2:i+2+rel], lookup)
			if err != nil {
				return "", err
			}
			b.WriteString(val)
			i += 2 + rel + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String(), nil
}

func resolvePlaceholder(expr string, lookup func(string) (string, bool)) (string, error) {
	name, def, hasDefault := strings.Cut(expr, ":")
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: empty variable name", ErrSubstitute)
	}
	v, ok := lookup(name)
	if hasDefault {
		if !ok || v == "" {
			return def, nil
		}
		return v, nil
	}
	if !ok {
		return "", fmt.Errorf("%w: %s is not set", ErrSubstitute, name)
	}
	return v, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `just test ./ops/config/...`
Expected: PASS.

- [ ] **Step 6: Format, lint, commit**

```bash
just fmt ./ops/config/...
just lint
git add ops/config/
git commit -m "feat(ops/config): \${VAR:default} substitution with empty-or-unset defaults"
```

---

## Task 4: `ops/config` — `.env` inheritance (`Dotenv`)

**Files:**
- Create: `ops/config/dotenv.go`
- Test: `ops/config/dotenv_test.go` (+ `ops/config/testdata/.env.local`, `ops/config/testdata/.env`)

**Interfaces:**
- Produces: `func Dotenv(paths ...string) error`; internal `func parseDotenv(path string) (map[string]string, error)`.

- [ ] **Step 1: Create fixtures**

`ops/config/testdata/.env.local`:

```
# base
HOST=localhost
PORT=3000
```

`ops/config/testdata/.env`:

```
PORT=8080
export TOKEN="a:b:c"
```

- [ ] **Step 2: Write the failing test**

```go
package config_test

import (
	"os"
	"testing"

	"github.com/dmitrymomot/forge/ops/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDotenv_InheritanceAndEnvWins(t *testing.T) {
	// real env wins over files
	t.Setenv("HOST", "realhost")

	require.NoError(t, config.Dotenv("testdata/.env.local", "testdata/.env"))

	assert.Equal(t, "realhost", os.Getenv("HOST"))     // real env preserved
	assert.Equal(t, "8080", os.Getenv("PORT"))         // later file (.env) overrides .env.local
	assert.Equal(t, "a:b:c", os.Getenv("TOKEN"))       // quoted value, export prefix stripped
}

func TestDotenv_MissingFile(t *testing.T) {
	assert.ErrorIs(t, config.Dotenv("testdata/nope.env"), config.ErrDotenv)
}
```

Note: `os.Setenv` from `t.Setenv` is restored after the test. `Dotenv` writes into the process env; keep keys used here distinct from other tests, or those tests set their own via `t.Setenv`.

- [ ] **Step 3: Run test to verify it fails**

Run: `just test ./ops/config/...`
Expected: FAIL — `Dotenv` undefined.

- [ ] **Step 4: Write the implementation**

Create `ops/config/dotenv.go`:

```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Dotenv loads .env files in order — later files override earlier ones — and
// applies the result to the process environment, but never overwrites a key
// already present in the real environment. So precedence is: process env >
// last file > ... > first file.
func Dotenv(paths ...string) error {
	merged := map[string]string{}
	for _, p := range paths {
		kv, err := parseDotenv(p)
		if err != nil {
			return err
		}
		for k, v := range kv {
			merged[k] = v
		}
	}
	for k, v := range merged {
		if _, present := os.LookupEnv(k); present {
			continue
		}
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("%w: %v", ErrDotenv, err)
		}
	}
	return nil
}

func parseDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDotenv, err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = unquoteEnv(strings.TrimSpace(v))
	}
	return out, nil
}

func unquoteEnv(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
	}
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	return v
}
```

(`strings.SplitSeq` is the Go 1.24+ iterator form modernize prefers; if lint objects, use `strings.Split`.)

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./ops/config/...
just fmt ./ops/config/...
just lint
git add ops/config/dotenv.go ops/config/dotenv_test.go ops/config/testdata/
git commit -m "feat(ops/config): .env inheritance with real-env precedence"
```

---

## Task 5: `ops/config` — profile detection & options

**Files:**
- Create: `ops/config/profile.go`, `ops/config/options.go`
- Test: `ops/config/profile_test.go`

**Interfaces:**
- Produces: `func Profile() string`, `IsDev/IsProd/IsTest/IsStaging() bool`; `type Option func(*config)`, `WithDotenv`, `WithProfile`, `WithFileName`, `WithLookup`; internal `config` struct with `newConfig`, `profile()`, `fileName()`, `lookup`, `applyDotenv()`.

- [ ] **Step 1: Write the failing test**

```go
package config_test

import (
	"testing"

	"github.com/dmitrymomot/forge/ops/config"
	"github.com/stretchr/testify/assert"
)

func TestProfilePredicates(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	assert.Equal(t, "production", config.Profile())
	assert.True(t, config.IsProd())
	assert.False(t, config.IsDev())

	t.Setenv("APP_ENV", "")
	t.Setenv("ENV", "")
	assert.Equal(t, "development", config.Profile()) // default
	assert.True(t, config.IsDev())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/config/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Write profile + predicates**

Create `ops/config/profile.go`:

```go
package config

import (
	"os"
	"slices"
	"strings"
)

// Profile returns the active deployment profile from APP_ENV, then ENV,
// defaulting to "development". The raw string is also the {profile}.yaml stem.
func Profile() string {
	for _, k := range []string{"APP_ENV", "ENV"} {
		if v, ok := os.LookupEnv(k); ok && v != "" {
			return v
		}
	}
	return "development"
}

func matches(p string, names ...string) bool {
	return slices.Contains(names, strings.ToLower(strings.TrimSpace(p)))
}

// IsDev reports whether the profile is a development profile.
func IsDev() bool { return matches(Profile(), "dev", "development", "local") }

// IsProd reports whether the profile is a production profile.
func IsProd() bool { return matches(Profile(), "prod", "production") }

// IsTest reports whether the profile is a test profile.
func IsTest() bool { return matches(Profile(), "test", "testing") }

// IsStaging reports whether the profile is a staging profile.
func IsStaging() bool { return matches(Profile(), "staging", "stage") }
```

- [ ] **Step 4: Write options + internal config**

Create `ops/config/options.go`:

```go
package config

import "os"

type config struct {
	dotenvPaths []string
	profile     string
	fileNameFn  func(profile string) string
	lookup      func(string) (string, bool)
}

func newConfig(opts ...Option) config {
	c := config{
		fileNameFn: func(p string) string { return p + ".yaml" },
		lookup:     os.LookupEnv,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (c config) activeProfile() string {
	if c.profile != "" {
		return c.profile
	}
	return Profile()
}

func (c config) fileName() string { return c.fileNameFn(c.activeProfile()) }

func (c config) applyDotenv() error {
	if len(c.dotenvPaths) == 0 {
		return nil
	}
	return Dotenv(c.dotenvPaths...)
}

// Option configures Load/LoadEnv/Populate.
type Option func(*config)

// WithDotenv loads these .env files (via Dotenv) before reading config.
func WithDotenv(paths ...string) Option {
	return func(c *config) { c.dotenvPaths = append(c.dotenvPaths, paths...) }
}

// WithProfile overrides APP_ENV/ENV detection for file selection.
func WithProfile(name string) Option { return func(c *config) { c.profile = name } }

// WithFileName customizes the profile→filename mapping (default profile+".yaml").
func WithFileName(fn func(profile string) string) Option {
	return func(c *config) {
		if fn != nil {
			c.fileNameFn = fn
		}
	}
}

// WithLookup overrides os.LookupEnv (test seam) for substitution and env reads.
func WithLookup(fn func(key string) (string, bool)) Option {
	return func(c *config) {
		if fn != nil {
			c.lookup = fn
		}
	}
}
```

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./ops/config/...
just fmt ./ops/config/...
just lint
git add ops/config/profile.go ops/config/options.go ops/config/profile_test.go
git commit -m "feat(ops/config): profile detection, predicates, and loader options"
```

---

## Task 6: `ops/config` — `LoadEnv` / `Populate` (env-tag decode)

**Files:**
- Create: `ops/config/env.go`
- Test: `ops/config/env_test.go`

**Interfaces:**
- Consumes: `structfields.Walk`, `structfields.SetString` (Task 1); the internal `config` (Task 5).
- Produces: `func LoadEnv[T any](opts ...Option) (T, error)`; `func Populate(dst any, opts ...Option) error`.

**Tag convention:** the env key is `env:"KEY"`; a default is the `default=` option (`env:"PORT,default=8080"`); required is the `required` option (`env:"PORT,required"`). Defaults live in the env tag (not a separate `default:` struct tag) because `structfields.Walk` surfaces only the walked tag key. Nested struct fields recurse, composing the prefix from the field's env tag name (`env:"DB"` → prefix `DB_`).

- [ ] **Step 1: Write the failing test**

```go
package config_test

import (
	"testing"

	"github.com/dmitrymomot/forge/ops/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dbCfg struct {
	Host string `env:"HOST,default=localhost"`
	Port int    `env:"PORT,default=5432"`
}

type appCfg struct {
	Name    string `env:"NAME,required"`
	Debug   bool   `env:"DEBUG,default=false"`
	DB      dbCfg  `env:"DB"`
}

func TestLoadEnv_DefaultsAndNesting(t *testing.T) {
	lookup := func(k string) (string, bool) {
		m := map[string]string{"NAME": "svc", "DB_PORT": "6543"}
		v, ok := m[k]
		return v, ok
	}
	got, err := config.LoadEnv[appCfg](config.WithLookup(lookup))
	require.NoError(t, err)
	assert.Equal(t, "svc", got.Name)
	assert.False(t, got.Debug)               // default
	assert.Equal(t, "localhost", got.DB.Host) // nested default
	assert.Equal(t, 6543, got.DB.Port)        // nested env override, prefixed DB_
}

func TestLoadEnv_RequiredMissing(t *testing.T) {
	lookup := func(string) (string, bool) { return "", false }
	_, err := config.LoadEnv[appCfg](config.WithLookup(lookup))
	assert.ErrorIs(t, err, config.ErrRequiredMissing)
}

func TestLoadEnv_ParseError(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == "NAME" {
			return "svc", true
		}
		if k == "DB_PORT" {
			return "not-int", true
		}
		return "", false
	}
	_, err := config.LoadEnv[appCfg](config.WithLookup(lookup))
	assert.ErrorIs(t, err, config.ErrParse)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/config/...`
Expected: FAIL — `LoadEnv` undefined.

- [ ] **Step 3: Write the implementation**

Create `ops/config/env.go`:

```go
package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/structfields"
)

// LoadEnv populates a fresh T from environment variables via its `env` tags.
func LoadEnv[T any](opts ...Option) (T, error) {
	var t T
	if err := Populate(&t, opts...); err != nil {
		return t, err
	}
	return t, nil
}

// Populate fills dst (a non-nil *struct) from the environment via `env` tags,
// applying `default=` options and failing on missing `required` keys. If dst
// implements interface{ Validate() error }, that runs afterward.
func Populate(dst any, opts ...Option) error {
	c := newConfig(opts...)
	if err := c.applyDotenv(); err != nil {
		return err
	}
	if err := populateStruct(dst, "", c.lookup); err != nil {
		return err
	}
	if v, ok := dst.(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func populateStruct(dst any, prefix string, lookup func(string) (string, bool)) error {
	var errs []error
	walkErr := structfields.Walk(dst, "env", func(f structfields.Field) error {
		if f.Tag.Ignored() {
			return nil
		}
		if f.Value.Kind() == reflect.Struct && f.Value.Type() != reflect.TypeOf(time.Time{}) {
			sub := prefix
			if f.Tag.Name != "" {
				sub = prefix + f.Tag.Name + "_"
			}
			if err := populateStruct(f.Value.Addr().Interface(), sub, lookup); err != nil {
				errs = append(errs, err)
			}
			return nil
		}
		if f.Tag.Name == "" {
			return nil
		}
		key := prefix + f.Tag.Name
		raw, ok := lookup(key)
		if !ok || raw == "" {
			def, hasDefault := defaultOption(f)
			switch {
			case hasDefault:
				raw = def
			case f.Tag.HasOption("required"):
				errs = append(errs, fmt.Errorf("%w: %s", ErrRequiredMissing, key))
				return nil
			default:
				return nil
			}
		}
		if err := structfields.SetString(f, raw); err != nil {
			errs = append(errs, fmt.Errorf("%w: %s: %v", ErrParse, key, err))
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	return errors.Join(errs...)
}

func defaultOption(f structfields.Field) (string, bool) {
	for _, o := range f.Tag.Options {
		if v, ok := strings.CutPrefix(o, "default="); ok {
			return v, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run tests, format, lint, commit**

```bash
just test ./ops/config/...
just fmt ./ops/config/...
just lint
git add ops/config/env.go ops/config/env_test.go
git commit -m "feat(ops/config): LoadEnv/Populate env-tag decode with defaults, required, nesting"
```

---

## Task 7: `ops/config` — `Load` (YAML per-env + substitution)

**Files:**
- Create: `ops/config/load.go`
- Modify: `go.mod`, `go.sum` (add `gopkg.in/yaml.v3`)
- Test: `ops/config/load_test.go` (+ `ops/config/testdata/development.yaml`, `ops/config/testdata/production.yaml`)

**Interfaces:**
- Consumes: internal `config` (Task 5), `substitute` (Task 3).
- Produces: `func Load[T any](dir string, opts ...Option) (T, error)`.

- [ ] **Step 1: Add the YAML dependency**

```bash
go get gopkg.in/yaml.v3@latest
```

Expected: `go.mod`/`go.sum` updated with `gopkg.in/yaml.v3`.

- [ ] **Step 2: Create fixtures**

`ops/config/testdata/development.yaml`:

```yaml
host: ${HOST:0.0.0.0}
port: ${PORT:8080}
name: dev-service
```

`ops/config/testdata/production.yaml`:

```yaml
host: ${HOST:0.0.0.0}
port: 443
name: prod-service
```

- [ ] **Step 3: Write the failing test**

```go
package config_test

import (
	"testing"

	"github.com/dmitrymomot/forge/ops/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serverCfg struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Name string `yaml:"name"`
}

func (s *serverCfg) SetDefaults() {
	if s.Host == "" {
		s.Host = "127.0.0.1"
	}
}

func TestLoad_YAMLWithSubstitution(t *testing.T) {
	t.Setenv("HOST", "10.0.0.5")

	got, err := config.Load[serverCfg]("testdata", config.WithProfile("development"))
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.5", got.Host) // ${HOST} substituted
	assert.Equal(t, 8080, got.Port)       // ${PORT:8080} default
	assert.Equal(t, "dev-service", got.Name)
}

func TestLoad_ProfileSelectionAndDefaults(t *testing.T) {
	t.Setenv("HOST", "") // empty -> ${HOST:0.0.0.0} uses default
	got, err := config.Load[serverCfg]("testdata", config.WithProfile("production"))
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0", got.Host)
	assert.Equal(t, 443, got.Port)
	assert.Equal(t, "prod-service", got.Name)
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load[serverCfg]("testdata", config.WithProfile("nope"))
	assert.ErrorIs(t, err, config.ErrProfileFile)
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `just test ./ops/config/...`
Expected: FAIL — `Load` undefined.

- [ ] **Step 5: Write the implementation**

Create `ops/config/load.go`:

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads {dir}/{profile}.yaml, expands ${VAR:default} placeholders against
// the environment layer, and unmarshals into T. If *T implements
// interface{ SetDefaults() } it is applied before decode (yaml overwrites only
// present keys); interface{ Validate() error } runs after.
func Load[T any](dir string, opts ...Option) (T, error) {
	var t T
	c := newConfig(opts...)
	if err := c.applyDotenv(); err != nil {
		return t, err
	}
	path := filepath.Join(dir, c.fileName())
	data, err := os.ReadFile(path)
	if err != nil {
		return t, fmt.Errorf("%w: %v", ErrProfileFile, err)
	}
	expanded, err := substitute(string(data), c.lookup)
	if err != nil {
		return t, err
	}
	if sd, ok := any(&t).(interface{ SetDefaults() }); ok {
		sd.SetDefaults()
	}
	if err := yaml.Unmarshal([]byte(expanded), &t); err != nil {
		return t, fmt.Errorf("%w: %v", ErrYAML, err)
	}
	if v, ok := any(&t).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return t, err
		}
	}
	return t, nil
}
```

- [ ] **Step 6: Run tests, format, lint, commit**

```bash
just test ./ops/config/...
just fmt ./ops/config/...
just lint
git add ops/config/load.go ops/config/load_test.go ops/config/testdata/ go.mod go.sum
git commit -m "feat(ops/config): Load YAML per-env with substitution (gopkg.in/yaml.v3)"
```

---

## Task 8: `ops/config` — `doc.go`

**Files:**
- Create: `ops/config/doc.go`

**Interfaces:** none (documentation).

- [ ] **Step 1: Write `doc.go`**

Create `ops/config/doc.go`. Lead with the import-alias note (verified compile error), then usage:

```go
// Package config layers application configuration from YAML (per-env convention
// with ${VAR:default} substitution), .env inheritance, and env-tagged structs.
//
// # Import alias required in packages that use the options idiom
//
// The package name "config" collides at compile time with the unexported
// package-level `type config struct` that forge's options idiom puts in nearly
// every package ("config already declared through import of package config").
// Any package that both uses that idiom and imports this loader must alias it:
//
//	import appconfig "github.com/dmitrymomot/forge/ops/config"
//
// # YAML per-env
//
//	// config/development.yaml:  host: ${HOST:0.0.0.0}
//	type Server struct {
//		Host string `yaml:"host"`
//		Port int    `yaml:"port"`
//	}
//	srv, err := config.Load[Server]("config/") // reads config/{APP_ENV}.yaml
//
// # .env inheritance (later files and the real env override earlier files)
//
//	err := config.Dotenv("config/.env.local", ".env")
//
// # Struct-from-env (12-factor, no YAML file)
//
//	type App struct {
//		Name string `env:"NAME,required"`
//		Port int    `env:"PORT,default=8080"`
//	}
//	app, err := config.LoadEnv[App](config.WithDotenv(".env"))
//
// ${VAR:default} uses the default when VAR is unset OR empty (shell :- form);
// ${VAR} with no default errors when unset. Profile() reads APP_ENV then ENV
// (default "development"); IsDev/IsProd/IsTest/IsStaging classify it.
package config
```

- [ ] **Step 2: Verify build + example compiles, format, lint, commit**

```bash
just test ./ops/config/...
just fmt ./ops/config/...
just lint
git add ops/config/doc.go
git commit -m "docs(ops/config): package doc with import-alias note and usage"
```

---

## Task 9: `resilience/ratelimit` — counter `Store` seam + memory store

**Files:**
- Create: `resilience/ratelimit/store.go`, `resilience/ratelimit/memory.go`
- Test: `resilience/ratelimit/memory_test.go`

**Interfaces:**
- Consumes: `core/clock.{Clock,System,NewMock}`.
- Produces: `type Store interface { Incr(ctx, key, delta, ttl) (int64, error); Get(ctx, key) (int64, error); Reset(ctx, key) error; Close() error }`; `func NewMemoryStore(opts ...MemoryOption) Store`; `type MemoryOption func(*memoryConfig)`; `WithMemoryClock(clock.Clock) MemoryOption`.

- [ ] **Step 1: Write the failing test**

```go
package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_IncrGetResetExpiry(t *testing.T) {
	mk := clock.NewMock(time.Unix(1000, 0))
	s := ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk))
	defer s.Close()
	ctx := context.Background()

	n, err := s.Incr(ctx, "k", 1, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = s.Incr(ctx, "k", 2, time.Minute) // does not extend TTL
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	got, err := s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(3), got)

	mk.Advance(61 * time.Second) // past TTL
	got, err = s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, int64(0), got) // expired

	got, _ = s.Get(ctx, "absent")
	assert.Equal(t, int64(0), got)
}

func TestMemoryStore_ConcurrentIncr(t *testing.T) {
	s := ratelimit.NewMemoryStore()
	defer s.Close()
	ctx := context.Background()
	const g = 100
	done := make(chan struct{}, g)
	for range g {
		go func() { _, _ = s.Incr(ctx, "c", 1, time.Minute); done <- struct{}{} }()
	}
	for range g {
		<-done
	}
	n, _ := s.Get(ctx, "c")
	assert.Equal(t, int64(g), n)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./resilience/ratelimit/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Write the seam**

Create `resilience/ratelimit/store.go`:

```go
package ratelimit

import (
	"context"
	"time"
)

// Store is the windowed atomic-counter seam shared by ratelimit (and, later,
// quota and lockout). It is distinct from cache.Store (byte-KV): counters need
// atomic increment-with-TTL, which Get/Set cannot express race-free.
type Store interface {
	// Incr atomically adds delta to key's counter and returns the new value. If
	// this call creates the key, its TTL is set to ttl; Incr never extends the
	// TTL of an existing (live) key.
	Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
	// Get returns the current counter, or 0 if the key is absent or expired.
	Get(ctx context.Context, key string) (int64, error)
	// Reset deletes the counter for key.
	Reset(ctx context.Context, key string) error
	// Close releases resources (e.g. a janitor goroutine).
	Close() error
}
```

- [ ] **Step 4: Write the memory store**

Create `resilience/ratelimit/memory.go`:

```go
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

type memoryConfig struct {
	clk clock.Clock
}

// MemoryOption configures NewMemoryStore.
type MemoryOption func(*memoryConfig)

// WithMemoryClock injects a clock (for tests). Default clock.System().
func WithMemoryClock(clk clock.Clock) MemoryOption {
	return func(c *memoryConfig) {
		if clk != nil {
			c.clk = clk
		}
	}
}

type counter struct {
	val       int64
	expiresAt time.Time
}

type memoryStore struct {
	mu  sync.Mutex
	m   map[string]counter
	clk clock.Clock
}

// NewMemoryStore returns an in-process counter Store. Lifecycle is the caller's
// (Close). Suitable for single-instance use and tests; multi-instance limiting
// needs ratelimit/redisstore.
func NewMemoryStore(opts ...MemoryOption) Store {
	c := memoryConfig{clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	return &memoryStore{m: make(map[string]counter), clk: c.clk}
}

func (s *memoryStore) Incr(_ context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok || now.After(e.expiresAt) {
		e = counter{val: delta, expiresAt: now.Add(ttl)}
	} else {
		e.val += delta
	}
	s.m[key] = e
	return e.val, nil
}

func (s *memoryStore) Get(_ context.Context, key string) (int64, error) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok || now.After(e.expiresAt) {
		return 0, nil
	}
	return e.val, nil
}

func (s *memoryStore) Reset(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *memoryStore) Close() error { return nil }
```

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./resilience/ratelimit/...
just fmt ./resilience/ratelimit/...
just lint
git add resilience/ratelimit/store.go resilience/ratelimit/memory.go resilience/ratelimit/memory_test.go
git commit -m "feat(resilience/ratelimit): counter Store seam and in-memory store"
```

---

## Task 10: `resilience/ratelimit` — sliding-window `Limiter`

**Files:**
- Create: `resilience/ratelimit/ratelimit.go`, `resilience/ratelimit/errors.go`
- Test: `resilience/ratelimit/ratelimit_test.go`

**Interfaces:**
- Consumes: `Store` (Task 9), `core/clock`.
- Produces: `type Limiter`, `func New(store Store, opts ...Option) *Limiter`, `func (*Limiter) Allow(ctx, key) (Result, error)`, `type Result`, `type Option`, `WithLimit(n int64, per time.Duration)`, `WithClock(clock.Clock)`, `var ErrLimited`.

- [ ] **Step 1: Write the failing test**

```go
package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimiter_AllowsUpToLimit(t *testing.T) {
	mk := clock.NewMock(time.Unix(0, 0))
	l := ratelimit.New(
		ratelimit.NewMemoryStore(ratelimit.WithMemoryClock(mk)),
		ratelimit.WithLimit(3, time.Minute),
		ratelimit.WithClock(mk),
	)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		res, err := l.Allow(ctx, "user")
		require.NoError(t, err)
		assert.True(t, res.Allowed, "request %d should pass", i)
		assert.Equal(t, int64(3), res.Limit)
	}
	res, err := l.Allow(ctx, "user")
	require.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.Equal(t, int64(0), res.Remaining)
	assert.Positive(t, res.RetryAfter)
}

func TestLimiter_KeysAreIsolated(t *testing.T) {
	l := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.WithLimit(1, time.Minute))
	ctx := context.Background()
	a, _ := l.Allow(ctx, "a")
	b, _ := l.Allow(ctx, "b")
	assert.True(t, a.Allowed)
	assert.True(t, b.Allowed)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./resilience/ratelimit/...`
Expected: FAIL — `New`, `Allow` undefined.

- [ ] **Step 3: Write the sentinel + limiter**

Create `resilience/ratelimit/errors.go`:

```go
package ratelimit

import "errors"

// ErrLimited indicates the subject exceeded its rate; used by the middleware's
// responder and available to non-HTTP callers.
var ErrLimited = errors.New("ratelimit: limit exceeded")
```

Create `resilience/ratelimit/ratelimit.go`:

```go
package ratelimit

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// Result reports a limiter decision for one key.
type Result struct {
	Allowed    bool
	Limit      int64
	Remaining  int64
	Reset      time.Time     // when the current window rolls
	RetryAfter time.Duration // 0 when Allowed
}

type config struct {
	limit  int64
	window time.Duration
	clk    clock.Clock
}

// Option configures a Limiter.
type Option func(*config)

// WithLimit sets n requests allowed per window. Required.
func WithLimit(n int64, per time.Duration) Option {
	return func(c *config) {
		c.limit = n
		c.window = per
	}
}

// WithClock injects a clock (for tests). Default clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// Limiter is a keyed sliding-window-counter limiter over a Store.
type Limiter struct {
	store Store
	cfg   config
}

// New builds a Limiter. The Store's lifecycle is the caller's.
func New(store Store, opts ...Option) *Limiter {
	c := config{limit: 100, window: time.Minute, clk: clock.System()}
	for _, o := range opts {
		o(&c)
	}
	if c.window <= 0 {
		c.window = time.Minute
	}
	return &Limiter{store: store, cfg: c}
}

// Allow records one hit against key and reports whether it is within the limit,
// using a weighted current+previous fixed-window estimate.
func (l *Limiter) Allow(ctx context.Context, key string) (Result, error) {
	now := l.cfg.clk.Now()
	win := now.Truncate(l.cfg.window)
	elapsed := now.Sub(win)

	curKey := key + ":" + strconv.FormatInt(win.Unix(), 10)
	prevKey := key + ":" + strconv.FormatInt(win.Add(-l.cfg.window).Unix(), 10)

	cur, err := l.store.Incr(ctx, curKey, 1, 2*l.cfg.window)
	if err != nil {
		return Result{}, err
	}
	prev, err := l.store.Get(ctx, prevKey)
	if err != nil {
		return Result{}, err
	}

	weight := 1 - float64(elapsed)/float64(l.cfg.window)
	est := float64(cur) + float64(prev)*weight

	res := Result{Limit: l.cfg.limit, Reset: win.Add(l.cfg.window)}
	if est > float64(l.cfg.limit) {
		res.Allowed = false
		res.Remaining = 0
		res.RetryAfter = res.Reset.Sub(now) // conservative: wait for the window to roll
		if res.RetryAfter < 0 {
			res.RetryAfter = 0
		}
		return res, nil
	}
	res.Allowed = true
	rem := l.cfg.limit - int64(math.Ceil(est))
	if rem < 0 {
		rem = 0
	}
	res.Remaining = rem
	return res, nil
}
```

- [ ] **Step 4: Run tests, format, lint, commit**

```bash
just test ./resilience/ratelimit/...
just fmt ./resilience/ratelimit/...
just lint
git add resilience/ratelimit/ratelimit.go resilience/ratelimit/errors.go resilience/ratelimit/ratelimit_test.go
git commit -m "feat(resilience/ratelimit): sliding-window Limiter over the counter Store"
```

---

## Task 11: `resilience/ratelimit` — HTTP middleware

**Files:**
- Create: `resilience/ratelimit/middleware.go`
- Test: `resilience/ratelimit/middleware_test.go`

**Interfaces:**
- Consumes: `Limiter` (Task 10), `web/middleware.Middleware`.
- Produces: `type KeyFunc func(*http.Request) string`; `func (*Limiter) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware`; `type MiddlewareOption`; `WithResponder(func(http.ResponseWriter, *http.Request, Result))`.

- [ ] **Step 1: Write the failing test**

```go
package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/resilience/ratelimit"
	"github.com/stretchr/testify/assert"
)

func TestMiddleware_HeadersAnd429(t *testing.T) {
	l := ratelimit.New(ratelimit.NewMemoryStore(), ratelimit.WithLimit(1, time.Minute))
	key := func(*http.Request) string { return "fixed" }
	h := l.Middleware(key)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Equal(t, "1", rec1.Header().Get("RateLimit-Limit"))

	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
	assert.NotEmpty(t, rec2.Header().Get("Retry-After"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./resilience/ratelimit/...`
Expected: FAIL — `Middleware` undefined.

- [ ] **Step 3: Write the middleware**

Create `resilience/ratelimit/middleware.go`:

```go
package ratelimit

import (
	"math"
	"net/http"
	"strconv"

	"github.com/dmitrymomot/forge/web/middleware"
)

// KeyFunc selects the limiter key for a request (e.g. client IP or user ID).
type KeyFunc func(*http.Request) string

type middlewareConfig struct {
	responder func(http.ResponseWriter, *http.Request, Result)
}

// MiddlewareOption configures Middleware.
type MiddlewareOption func(*middlewareConfig)

// WithResponder overrides the 429 response (default plain text). Use it to emit
// problem+json via web/problem.
func WithResponder(fn func(http.ResponseWriter, *http.Request, Result)) MiddlewareOption {
	return func(c *middlewareConfig) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// Middleware limits by key, emitting RateLimit-* headers and a 429 (with
// Retry-After) when exceeded. On a Store error it fails open (serves the
// request) to avoid turning a limiter outage into an app outage.
func (l *Limiter) Middleware(key KeyFunc, opts ...MiddlewareOption) middleware.Middleware {
	cfg := middlewareConfig{responder: defaultResponder}
	for _, o := range opts {
		o(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res, err := l.Allow(r.Context(), key(r))
			if err != nil {
				next.ServeHTTP(w, r) // fail open
				return
			}
			writeRateLimitHeaders(w, res)
			if !res.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(res.RetryAfter.Seconds()))))
				cfg.responder(w, r, res)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeRateLimitHeaders(w http.ResponseWriter, res Result) {
	w.Header().Set("RateLimit-Limit", strconv.FormatInt(res.Limit, 10))
	w.Header().Set("RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
	reset := int(math.Ceil(res.RetryAfter.Seconds()))
	if res.Allowed {
		reset = 0
	}
	w.Header().Set("RateLimit-Reset", strconv.Itoa(reset))
}

func defaultResponder(w http.ResponseWriter, _ *http.Request, _ Result) {
	http.Error(w, ErrLimited.Error(), http.StatusTooManyRequests)
}
```

- [ ] **Step 4: Run tests, format, lint, commit**

```bash
just test ./resilience/ratelimit/...
just fmt ./resilience/ratelimit/...
just lint
git add resilience/ratelimit/middleware.go resilience/ratelimit/middleware_test.go
git commit -m "feat(resilience/ratelimit): HTTP middleware with RateLimit-* headers and 429"
```

---

## Task 12: `resilience/ratelimit/redisstore` — Redis counter Store

**Files:**
- Create: `resilience/ratelimit/redisstore/redisstore.go`, `resilience/ratelimit/redisstore/doc.go`
- Test: `resilience/ratelimit/redisstore/redisstore_test.go`

**Interfaces:**
- Consumes: `github.com/redis/go-redis/v9`, the `ratelimit.Store` contract.
- Produces: `func New(client redis.UniversalClient, opts ...Option) *Store`; `*Store` implements `ratelimit.Store`; `type Option`, `WithPrefix(string)`.

**Redis gating:** the test needs a live Redis. Mirror `data/redis`'s integration test gating — read the address from an env var (e.g. `REDIS_ADDR`) and `t.Skip` when unset. Check `data/redis/*_test.go` for the exact env var/helper this repo uses and reuse it.

- [ ] **Step 1: Write the failing test**

```go
package redisstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/resilience/ratelimit/redisstore"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dial(t *testing.T) redis.UniversalClient {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisStore_IncrTTLAtomic(t *testing.T) {
	client := dial(t)
	s := redisstore.New(client, redisstore.WithPrefix("rltest:"))
	ctx := context.Background()
	key := "k-" + t.Name()
	require.NoError(t, s.Reset(ctx, key))

	n, err := s.Incr(ctx, key, 1, 500*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	n, err = s.Incr(ctx, key, 1, 500*time.Millisecond) // must NOT re-arm TTL
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	time.Sleep(600 * time.Millisecond)
	got, err := s.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got) // expired at the FIRST incr's TTL, not extended
}
```

- [ ] **Step 2: Run test to verify it fails (or skips)**

Run: `REDIS_ADDR= just test ./resilience/ratelimit/redisstore/...`
Expected: FAIL to compile (undefined) — after implementation, SKIP without `REDIS_ADDR`, PASS with it.

- [ ] **Step 3: Write the implementation**

Create `resilience/ratelimit/redisstore/redisstore.go`:

```go
package redisstore

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrScript increments the key and sets the TTL only on creation (when the new
// value equals the delta), so a window's expiry is fixed at its first hit.
var incrScript = redis.NewScript(`
local v = redis.call('INCRBY', KEYS[1], ARGV[1])
if v == tonumber(ARGV[1]) then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return v`)

type config struct {
	prefix string
}

// Option configures the Store.
type Option func(*config)

// WithPrefix namespaces all keys (e.g. "rl:").
func WithPrefix(p string) Option { return func(c *config) { c.prefix = p } }

// Store implements ratelimit.Store over a go-redis client. The client's
// lifecycle is the caller's; Close is a no-op.
type Store struct {
	client redis.UniversalClient
	prefix string
}

// New builds a Redis-backed counter Store.
func New(client redis.UniversalClient, opts ...Option) *Store {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return &Store{client: client, prefix: c.prefix}
}

func (s *Store) key(k string) string { return s.prefix + k }

// Incr atomically adds delta and sets ttl only when the key is newly created.
func (s *Store) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	return incrScript.Run(ctx, s.client, []string{s.key(key)}, delta, ttl.Milliseconds()).Int64()
}

// Get returns the counter, or 0 when absent.
func (s *Store) Get(ctx context.Context, key string) (int64, error) {
	n, err := s.client.Get(ctx, s.key(key)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

// Reset deletes the counter.
func (s *Store) Reset(ctx context.Context, key string) error {
	return s.client.Del(ctx, s.key(key)).Err()
}

// Close is a no-op; the client is owned by the caller.
func (s *Store) Close() error { return nil }
```

Create `resilience/ratelimit/redisstore/doc.go`:

```go
// Package redisstore implements ratelimit.Store over a go-redis client, so a
// sliding-window Limiter shares one counter across instances. Incr is a Lua
// script that INCRBYs and sets the TTL only on the window's first hit; the
// client's lifecycle stays with the caller.
//
//	store := redisstore.New(client, redisstore.WithPrefix("rl:"))
//	limiter := ratelimit.New(store, ratelimit.WithLimit(100, time.Minute))
package redisstore
```

- [ ] **Step 4: Add a compile-time interface assertion**

Add to `redisstore.go` (ensures `*Store` satisfies the seam without importing it into the package API):

```go
// (in redisstore_test.go, black-box) — assert the contract:
// var _ ratelimit.Store = (*redisstore.Store)(nil)
```

Put that assertion in `redisstore_test.go`:

```go
var _ ratelimit.Store = (*redisstore.Store)(nil)
```

(add the `ratelimit` import to the test file).

- [ ] **Step 5: Run tests (skips without Redis), format, lint, commit**

```bash
just test ./resilience/ratelimit/redisstore/...
just fmt ./resilience/ratelimit/redisstore/...
just lint
git add resilience/ratelimit/redisstore/ go.mod go.sum
git commit -m "feat(resilience/ratelimit/redisstore): Redis counter Store with atomic per-window TTL"
```

---

## Task 13: `ops/health` — pull handler + checks

**Files:**
- Create: `ops/health/health.go`, `ops/health/errors.go`
- Test: `ops/health/health_test.go`

**Interfaces:**
- Produces: `func Handler(opts ...Option) http.Handler`; `type Check func(context.Context) error`; `type Option`, `WithCheck(name, Check, ...CheckOption)`, `WithTimeout(time.Duration)`, `WithResponder(func(http.ResponseWriter, *http.Request, Report))`; `type CheckOption`, `NonCritical()`; `type Report`, `type CheckResult`.

- [ ] **Step 1: Write the failing test**

```go
package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func do(h http.Handler) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec
}

func TestHandler_LivenessNoChecks(t *testing.T) {
	rec := do(health.Handler())
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandler_CriticalFailureIs503(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("ok", func(context.Context) error { return nil }),
		health.WithCheck("db", func(context.Context) error { return errors.New("down") }),
	))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var rep health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	assert.Equal(t, "unavailable", rep.Status)
}

func TestHandler_NonCriticalDegrades200(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("cache", func(context.Context) error { return errors.New("down") }, health.NonCritical()),
	))
	assert.Equal(t, http.StatusOK, rec.Code)
	var rep health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rep))
	assert.Equal(t, "degraded", rep.Status)
}

func TestHandler_TimeoutIsFailureNotHang(t *testing.T) {
	rec := do(health.Handler(
		health.WithCheck("slow", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}),
		health.WithTimeout(20*time.Millisecond),
	))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/health/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Write errors + handler**

Create `ops/health/errors.go`:

```go
package health

import "errors"

// ErrDraining is returned by a down Gate's Check while the app is shutting down.
var ErrDraining = errors.New("health: draining")
```

Create `ops/health/health.go`:

```go
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Check reports the health of one dependency; nil means healthy.
type Check func(ctx context.Context) error

// Report is the aggregate result of one scrape.
type Report struct {
	Status string        `json:"status"` // "ok" | "degraded" | "unavailable"
	Checks []CheckResult `json:"checks"`
}

// CheckResult is one check's outcome.
type CheckResult struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Critical bool   `json:"critical"`
	Err      string `json:"err,omitempty"`
}

type checkEntry struct {
	name     string
	check    Check
	critical bool
}

type config struct {
	checks    []checkEntry
	timeout   time.Duration
	responder func(http.ResponseWriter, *http.Request, Report)
}

// Option configures a Handler.
type Option func(*config)

type checkConfig struct{ critical bool }

// CheckOption configures a single check.
type CheckOption func(*checkConfig)

// NonCritical marks a check as degrade-not-evict: its failure yields a
// "degraded" 200 instead of a 503.
func NonCritical() CheckOption { return func(c *checkConfig) { c.critical = false } }

// WithCheck registers a named check. Checks are critical by default.
func WithCheck(name string, check Check, opts ...CheckOption) Option {
	cc := checkConfig{critical: true}
	for _, o := range opts {
		o(&cc)
	}
	return func(c *config) {
		c.checks = append(c.checks, checkEntry{name: name, check: check, critical: cc.critical})
	}
}

// WithTimeout bounds each check's context per scrape. 0 inherits the request ctx.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithResponder overrides the default JSON body/format.
func WithResponder(fn func(http.ResponseWriter, *http.Request, Report)) Option {
	return func(c *config) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// Handler returns an http.Handler that runs every registered check on each
// request and reports the aggregate. With no checks it always returns 200 — the
// canonical liveness probe.
func Handler(opts ...Option) http.Handler {
	c := config{responder: defaultResponder}
	for _, o := range opts {
		o(&c)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.responder(w, r, evaluate(r.Context(), c))
	})
}

func evaluate(ctx context.Context, c config) Report {
	results := make([]CheckResult, len(c.checks))
	var wg sync.WaitGroup
	for i, e := range c.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx := ctx
			if c.timeout > 0 {
				var cancel context.CancelFunc
				cctx, cancel = context.WithTimeout(ctx, c.timeout)
				defer cancel()
			}
			err := e.check(cctx)
			results[i] = CheckResult{Name: e.name, OK: err == nil, Critical: e.critical}
			if err != nil {
				results[i].Err = err.Error()
			}
		}()
	}
	wg.Wait()
	return summarize(results)
}

func summarize(results []CheckResult) Report {
	status := "ok"
	for _, r := range results {
		if r.OK {
			continue
		}
		if r.Critical {
			return Report{Status: "unavailable", Checks: results}
		}
		status = "degraded"
	}
	return Report{Status: status, Checks: results}
}

func defaultResponder(w http.ResponseWriter, _ *http.Request, rep Report) {
	code := http.StatusOK
	if rep.Status == "unavailable" {
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(rep)
}
```

- [ ] **Step 4: Run tests, format, lint, commit**

```bash
just test ./ops/health/...
just fmt ./ops/health/...
just lint
git add ops/health/health.go ops/health/errors.go ops/health/health_test.go
git commit -m "feat(ops/health): pull handler with critical/degraded checks and JSON report"
```

---

## Task 14: `ops/health` — `Gate` + `doc.go`

**Files:**
- Create: `ops/health/gate.go`, `ops/health/doc.go`
- Test: `ops/health/gate_test.go`

**Interfaces:**
- Consumes: `ErrDraining` (Task 13).
- Produces: `type Gate`, `func NewGate() *Gate`, `func (*Gate) Check(context.Context) error`, `Up()`, `Down()`.

- [ ] **Step 1: Write the failing test**

```go
package health_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/dmitrymomot/forge/ops/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGate_FlipsReadiness(t *testing.T) {
	gate := health.NewGate()
	require.NoError(t, gate.Check(context.Background())) // up by default

	h := health.Handler(health.WithCheck("accepting", gate.Check))

	rec := do(h)
	assert.Equal(t, http.StatusOK, rec.Code)

	gate.Down()
	assert.ErrorIs(t, gate.Check(context.Background()), health.ErrDraining)

	rec = do(h)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	gate.Up()
	rec = do(h)
	assert.Equal(t, http.StatusOK, rec.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/health/...`
Expected: FAIL — `NewGate` undefined.

- [ ] **Step 3: Write the Gate**

Create `ops/health/gate.go`:

```go
package health

import (
	"context"
	"sync/atomic"
)

// Gate is a flippable readiness signal exposed as a Check — the drain primitive.
// It starts up (healthy); Down makes its Check report ErrDraining.
type Gate struct {
	up atomic.Bool
}

// NewGate returns a Gate in the "up" state.
func NewGate() *Gate {
	g := &Gate{}
	g.up.Store(true)
	return g
}

// Check reports nil while up, ErrDraining once Down.
func (g *Gate) Check(context.Context) error {
	if g.up.Load() {
		return nil
	}
	return ErrDraining
}

// Up marks the gate healthy.
func (g *Gate) Up() { g.up.Store(true) }

// Down marks the gate draining.
func (g *Gate) Down() { g.up.Store(false) }
```

- [ ] **Step 4: Write `doc.go` (four-store example + drain)**

Create `ops/health/doc.go`. The multi-store example is a documentation comment (not compiled), so `health` takes no `data/*` dependency:

```go
// Package health is a single pull-evaluated handler factory for liveness and
// readiness. Handler() with no checks always returns 200 (liveness); the same
// function with checks is readiness. Checks run on each scrape (no cache, no
// background workers); they are critical by default (failure → 503) unless
// marked NonCritical (failure → "degraded" 200).
//
// # Liveness + readiness over datastores
//
// Each forge data client already exposes a func(ctx) error healthcheck, which
// IS a health.Check — so they plug straight in:
//
//	mux.Handle("GET /livez", health.Handler()) // process is up
//
//	mux.Handle("GET /readyz", health.Handler(
//		health.WithCheck("postgres", postgres.Healthcheck(pool)),                // critical
//		health.WithCheck("mongo", mongo.Healthcheck(mdb)),                       // critical
//		health.WithCheck("redis", redis.Healthcheck(rdb), health.NonCritical()), // degrade
//		health.WithCheck("opensearch", opensearch.Healthcheck(osc), health.NonCritical()),
//		health.WithTimeout(2*time.Second),
//	))
//
// # Graceful drain (readiness 503 before the server stops)
//
// A Gate check plus supervisor's ordered pre-shutdown phase flips /readyz to 503
// and waits so the load balancer deregisters while the server still serves:
//
//	gate := health.NewGate()
//	readyz := health.Handler(health.WithCheck("accepting", gate.Check) /* + store checks */)
//	supervisor.Run(ctx,
//		supervisor.WithPreShutdown("drain", func(ctx context.Context) {
//			gate.Down() // next /readyz scrape is 503; server keeps serving
//			select {
//			case <-time.After(5 * time.Second): // ≥ probeInterval × failureThreshold
//			case <-ctx.Done():
//			}
//		}),
//		supervisor.WithService(srv), // listener closes only after the hook returns
//	)
//
// An HTTP-dependency check is a one-liner (GET → 2xx, drain+close body, honor
// ctx); it is not a package export.
package health
```

- [ ] **Step 5: Run tests, format, lint, commit**

```bash
just test ./ops/health/...
just fmt ./ops/health/...
just lint
git add ops/health/gate.go ops/health/doc.go ops/health/gate_test.go
git commit -m "feat(ops/health): Gate drain primitive and package doc with datastore + drain examples"
```

---

## Task 15: `web/httpclient` — resilient transport (retry + timeouts + hooks + propagation)

**Files:**
- Create: `web/httpclient/httpclient.go`, `web/httpclient/options.go`
- Test: `web/httpclient/httpclient_test.go`

**Interfaces:**
- Consumes: `resilience/retry`, `resilience/backoff`.
- Produces: `func New(opts ...Option) *http.Client`; `type Option`; `WithTimeout`, `WithPerAttemptTimeout`, `WithRetry(...retry.Option)`, `WithRetryMethods(...string)`, `WithBaseTransport(http.RoundTripper)`, `WithBefore`, `WithAfter`, `WithContextHeaders`, `WithHeader`, `WithUserAgent`. (Breaker option added in Task 16.)

- [ ] **Step 1: Write the failing test**

```go
package httpclient_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dmitrymomot/forge/web/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_RetriesTransient503ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := httpclient.New()
	resp, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), calls.Load())
}

func TestNew_DoesNotRetryPOSTByDefault(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := httpclient.New()
	resp, err := client.Post(srv.URL, "text/plain", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, int32(1), calls.Load()) // POST not retried
}

func TestNew_PropagatesContextHeaders(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := httpclient.New(
		httpclient.WithContextHeaders(func(ctx context.Context) http.Header {
			h := http.Header{}
			if v, ok := ctx.Value(ctxKey{}).(string); ok {
				h.Set("X-Request-ID", v)
			}
			return h
		}),
	)
	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), ctxKey{}, "rid-1"), http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	assert.Equal(t, "rid-1", got)
}

type ctxKey struct{}
```

(Add `context` to the test imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/httpclient/...`
Expected: FAIL — undefined.

- [ ] **Step 3: Write options**

Create `web/httpclient/options.go`:

```go
package httpclient

import (
	"context"
	"net/http"
	"time"

	"github.com/dmitrymomot/forge/resilience/retry"
)

type config struct {
	timeout      time.Duration
	perAttempt   time.Duration
	retryOpts    []retry.Option
	retryMethods map[string]bool
	base         http.RoundTripper
	before       []func(*http.Request)
	after        []func(*http.Request, *http.Response)
	ctxHeaders   []func(context.Context) http.Header
	headers      http.Header
	userAgent    string
	useBreaker   bool
	breakerOpts  []any // populated in Task 16 (circuitbreaker.GroupOption)
}

func newConfig(opts ...Option) config {
	c := config{
		base:         http.DefaultTransport,
		retryMethods: map[string]bool{http.MethodGet: true, http.MethodHead: true, http.MethodPut: true, http.MethodDelete: true, http.MethodOptions: true},
		headers:      http.Header{},
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Option configures the client.
type Option func(*config)

// WithTimeout sets the overall http.Client timeout.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithPerAttemptTimeout bounds each individual attempt via context.
func WithPerAttemptTimeout(d time.Duration) Option { return func(c *config) { c.perAttempt = d } }

// WithRetry tunes the retry policy (default 3 attempts, jittered backoff).
func WithRetry(opts ...retry.Option) Option { return func(c *config) { c.retryOpts = append(c.retryOpts, opts...) } }

// WithRetryMethods sets which HTTP methods are retried (default idempotent: GET,HEAD,PUT,DELETE,OPTIONS).
func WithRetryMethods(methods ...string) Option {
	return func(c *config) {
		c.retryMethods = map[string]bool{}
		for _, m := range methods {
			c.retryMethods[m] = true
		}
	}
}

// WithBaseTransport sets the innermost RoundTripper (default http.DefaultTransport).
func WithBaseTransport(rt http.RoundTripper) Option {
	return func(c *config) {
		if rt != nil {
			c.base = rt
		}
	}
}

// WithBefore runs fn on each outbound request before it is sent.
func WithBefore(fn func(*http.Request)) Option { return func(c *config) { c.before = append(c.before, fn) } }

// WithAfter runs fn after each response is received.
func WithAfter(fn func(*http.Request, *http.Response)) Option { return func(c *config) { c.after = append(c.after, fn) } }

// WithContextHeaders copies headers derived from the request context onto the
// outbound request (e.g. X-Request-ID, traceparent).
func WithContextHeaders(fn func(context.Context) http.Header) Option {
	return func(c *config) { c.ctxHeaders = append(c.ctxHeaders, fn) }
}

// WithHeader sets a static header on every request.
func WithHeader(key, value string) Option { return func(c *config) { c.headers.Set(key, value) } }

// WithUserAgent sets the User-Agent header on every request.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }
```

- [ ] **Step 4: Write the transport**

Create `web/httpclient/httpclient.go`:

```go
package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/resilience/retry"
)

// New returns a resilient *http.Client: a RoundTripper stack that decorates the
// request (static/context headers, hooks), retries idempotent methods on
// transient failures with jittered backoff (honoring Retry-After), and bounds
// each attempt. It returns the stdlib type; problem+json surfacing is a
// companion problem.Decode(resp) call.
func New(opts ...Option) *http.Client {
	c := newConfig(opts...)
	return &http.Client{Transport: &transport{cfg: c, breaker: buildBreaker(c)}, Timeout: c.timeout}
}

type transport struct {
	cfg     config
	breaker breakerFunc // nil unless WithBreakerGroup set (Task 16)
}

// breakerFunc runs fn under a per-host breaker; nil means no breaker.
type breakerFunc func(ctx context.Context, host string, fn func(context.Context) error) error

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.decorate(req)

	var resp *http.Response
	attempt := func(ctx context.Context) error {
		r := req.Clone(ctx)
		if req.GetBody != nil { // replay the body on retried methods (PUT/DELETE with a body)
			b, gErr := req.GetBody()
			if gErr != nil {
				return retry.Permanent(gErr)
			}
			r.Body = b
		}
		call := func(ctx context.Context) error {
			cctx := ctx
			if t.cfg.perAttempt > 0 {
				var cancel context.CancelFunc
				cctx, cancel = context.WithTimeout(ctx, t.cfg.perAttempt)
				defer cancel()
			}
			out, err := t.cfg.base.RoundTrip(r.WithContext(cctx))
			if err != nil {
				return err
			}
			resp = out
			for _, fn := range t.cfg.after {
				fn(r, out)
			}
			if retryableStatus(out.StatusCode) {
				return statusError{code: out.StatusCode, retryAfter: parseRetryAfter(out)}
			}
			return nil
		}
		if t.breaker != nil {
			return t.breaker(ctx, req.URL.Host, call)
		}
		return call(ctx)
	}

	ctx := req.Context()
	var err error
	if t.cfg.retryMethods[req.Method] {
		err = retry.Do(ctx, attempt, append([]retry.Option{retry.WithRetryIf(isRetryable)}, t.cfg.retryOpts...)...)
	} else {
		err = attempt(ctx)
	}

	// A bad-status "error" is internal to drive retry; return the response, not an error.
	if _, ok := errors.AsType[statusError](err); ok {
		return resp, nil
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (t *transport) decorate(req *http.Request) {
	for k, vs := range t.cfg.headers {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	if t.cfg.userAgent != "" {
		req.Header.Set("User-Agent", t.cfg.userAgent)
	}
	for _, fn := range t.cfg.ctxHeaders {
		for k, vs := range fn(req.Context()) {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
	}
	for _, fn := range t.cfg.before {
		fn(req)
	}
}

// statusError carries a retryable HTTP status and any Retry-After hint, so
// retry (which honors retry.RetryAfterError) can raise the delay floor.
type statusError struct {
	code       int
	retryAfter time.Duration
}

func (e statusError) Error() string          { return fmt.Sprintf("httpclient: server returned %d", e.code) }
func (e statusError) RetryAfter() time.Duration { return e.retryAfter }

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

func isRetryable(err error) bool {
	if _, ok := errors.AsType[statusError](err); ok {
		return true
	}
	// network/transport errors are retryable; context cancellation is not.
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func parseRetryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}
```

- [ ] **Step 5: Add a no-op breaker builder (real one in Task 16)**

Create `web/httpclient/breaker.go`:

```go
package httpclient

// buildBreaker returns nil until WithBreakerGroup is wired in Task 16.
func buildBreaker(c config) breakerFunc { return nil }
```

- [ ] **Step 6: Run tests, format, lint, commit**

```bash
just test ./web/httpclient/...
just fmt ./web/httpclient/...
just lint
git add web/httpclient/httpclient.go web/httpclient/options.go web/httpclient/breaker.go web/httpclient/httpclient_test.go
git commit -m "feat(web/httpclient): resilient transport with retry, timeouts, hooks, propagation"
```

---

## Task 16: `web/httpclient` — opt-in per-host breaker + `doc.go`

**Files:**
- Modify: `web/httpclient/options.go` (add `WithBreakerGroup`), `web/httpclient/breaker.go` (build the Group)
- Create: `web/httpclient/doc.go`
- Test: `web/httpclient/breaker_test.go`

**Interfaces:**
- Consumes: `resilience/circuitbreaker.{NewGroup,Group,GroupOption,ErrOpen}`.
- Produces: `func WithBreakerGroup(opts ...circuitbreaker.GroupOption) Option`.

- [ ] **Step 1: Write the failing test**

```go
package httpclient_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
	"github.com/dmitrymomot/forge/web/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBreaker_OptInTripsAfterFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := httpclient.New(
		httpclient.WithRetryMethods(), // disable retry to isolate the breaker
		httpclient.WithBreakerGroup(circuitbreaker.WithBreakerOptions(circuitbreaker.WithFailureThreshold(2))),
	)
	// Drive failures past the threshold.
	for range 3 {
		resp, err := client.Get(srv.URL)
		if err == nil {
			resp.Body.Close()
		}
	}
	// Next call should fast-fail with ErrOpen.
	_, err := client.Get(srv.URL)
	require.Error(t, err)
	assert.True(t, errors.Is(err, circuitbreaker.ErrOpen))
}

func TestBreaker_OffByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := httpclient.New(httpclient.WithRetryMethods())
	for range 10 {
		resp, err := client.Get(srv.URL)
		require.NoError(t, err) // 500 is a response, not an error; never ErrOpen
		resp.Body.Close()
	}
}
```

Verify the exact circuitbreaker option names against `resilience/circuitbreaker/group.go` and `circuitbreaker.go` (`WithBreakerOptions`, `WithFailureThreshold`) before running; adjust the test to the real names if they differ.

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./web/httpclient/...`
Expected: FAIL — `WithBreakerGroup` undefined.

- [ ] **Step 3: Add the option**

In `web/httpclient/options.go`, add the import `"github.com/dmitrymomot/forge/resilience/circuitbreaker"`, change the `breakerOpts` field type from `[]any` to `[]circuitbreaker.GroupOption`, and add:

```go
// WithBreakerGroup enables a per-host circuit breaker (off by default). Each
// attempt runs inside a circuitbreaker.Group keyed by request host; the breaker's
// open error carries Retry-After, so retry and breaker cooperate.
func WithBreakerGroup(opts ...circuitbreaker.GroupOption) Option {
	return func(c *config) {
		c.useBreaker = true
		c.breakerOpts = append(c.breakerOpts, opts...)
	}
}
```

- [ ] **Step 4: Build the breaker**

Replace `web/httpclient/breaker.go`:

```go
package httpclient

import (
	"context"

	"github.com/dmitrymomot/forge/resilience/circuitbreaker"
)

// buildBreaker returns a per-host breaker func, or nil when the breaker is off.
func buildBreaker(c config) breakerFunc {
	if !c.useBreaker {
		return nil
	}
	group := circuitbreaker.NewGroup(c.breakerOpts...)
	return func(ctx context.Context, host string, fn func(context.Context) error) error {
		return group.Do(ctx, host, fn)
	}
}
```

- [ ] **Step 5: Write `doc.go`**

Create `web/httpclient/doc.go`:

```go
// Package httpclient builds a resilient *http.Client (the stdlib type) from a
// RoundTripper stack over the shipped retry/backoff/circuitbreaker packages:
// static + context-derived headers and before/after hooks, jittered retry of
// idempotent methods on transient failures (5xx/429/network, honoring
// Retry-After), a per-attempt timeout, and an OPT-IN per-host circuit breaker.
//
//	client := httpclient.New(
//		httpclient.WithPerAttemptTimeout(2*time.Second),
//		httpclient.WithContextHeaders(func(ctx context.Context) http.Header {
//			h := http.Header{}
//			if id, ok := requestid.From(ctx); ok {
//				h.Set("X-Request-ID", id)
//			}
//			return h
//		}),
//		httpclient.WithBreakerGroup(), // enable per-host breaker
//	)
//
//	resp, err := client.Get(url)
//	if err != nil { /* transport/breaker error */ }
//	if err := problem.Decode(resp); err != nil { /* 4xx/5xx problem+json */ }
//
// Retry is on by default (3 attempts) for GET/HEAD/PUT/DELETE/OPTIONS only —
// POST is excluded to avoid silent double-submits (override with
// WithRetryMethods). New returns the stdlib *http.Client, so problem surfacing
// is the companion problem.Decode call rather than a changed Do signature.
package httpclient
```

- [ ] **Step 6: Run tests, format, lint, commit**

```bash
just test ./web/httpclient/...
just fmt ./web/httpclient/...
just lint
git add web/httpclient/options.go web/httpclient/breaker.go web/httpclient/doc.go web/httpclient/breaker_test.go
git commit -m "feat(web/httpclient): opt-in per-host circuit breaker and package doc"
```

---

## Task 17: Update `docs/packages.md` + full verification

**Files:**
- Modify: `docs/packages.md`

- [ ] **Step 1: Move the four packages to shipped**

In `docs/packages.md`: move `config` (as `ops/config`), `health`, `ratelimit` (+`ratelimit/redisstore`), and `httpclient` from *planned* to *shipped* in their domain lists; update the shipped-count header line; drop the Wave 1 rows from the build-order section. Note the new `gopkg.in/yaml.v3` isolated dep in the dependency-philosophy section.

- [ ] **Step 2: Full-tree verification**

```bash
just test ./...
just lint
go tool modernize ./...
```

Expected: all tests PASS (race + cover), lint clean, modernize reports no changes. Fix anything that surfaces.

- [ ] **Step 3: Commit**

```bash
git add docs/packages.md
git commit -m "docs(packages): mark Wave 1 completion bundle shipped (config, health, ratelimit, httpclient)"
```

---

## Self-Review Notes (author checklist — verify during execution)

- **Spec coverage:** SetString (§0.1)→T1; WithPreShutdown (§0.2)→T2; config substitution/dotenv/profile/LoadEnv/Load/doc (§1)→T3–T8; ratelimit seam/limiter/middleware/redisstore (§3)→T9–T12; health handler/Gate/doc (§2)→T13–T14; httpclient transport/breaker/doc (§4)→T15–T16; packages.md + verification→T17. All spec sections mapped.
- **Deviations from spec (intentional, within the structfields seam):** (a) env-tag defaults use the `default=` tag OPTION (`env:"PORT,default=8080"`) not a separate `default:"…"` struct tag — `structfields.Walk` surfaces only the walked tag; (b) nested-struct prefix derives from the field's `env` tag name (`env:"DB"` → `DB_`) rather than a `config:"prefix=…"` tag, for the same reason; (c) `SetString` slices support `[]string` only in v1. Update spec §1 if these should be reconciled there.
- **Type consistency:** `Store` signatures identical across `store.go`, `memory.go`, `redisstore` (`Incr(ctx,key,delta int64,ttl)`, `Get`, `Reset`, `Close`). `breakerFunc`/`buildBreaker` signature matches between T15 (no-op) and T16 (real). `Result`, `Report`, `CheckResult` field names match their tests.
- **Verify-before-run gotchas flagged inline:** exact `circuitbreaker` option names (T16), `data/redis` integration-test gating pattern (T12), and `strings.SplitSeq` vs `strings.Split` under lint (T4).
