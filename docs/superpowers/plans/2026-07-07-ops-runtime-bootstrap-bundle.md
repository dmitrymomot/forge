# Ops Runtime-Bootstrap Bundle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship four `ops/` packages — `buildinfo`, `automaxprocs`, `logredact`, `bootstrap` — plus a `supervisor.WithContext` addition, giving a forge app a one-line `main()` with runtime tuning, build identity, log redaction, and generic config autoload.

**Architecture:** Three independent leaf packages (`buildinfo`, `automaxprocs`, `logredact`) and one shipped-package addition (`supervisor.WithContext`) are built first, in any order. `bootstrap` composes all of them plus `logger`/`config` into `Run` / `RunWithConfig[T]`, delegating signal handling to `supervisor.NewContext` and owning only the runtime edges (logger → automaxprocs → buildinfo log → config load → signal ctx → exit code). The app body it calls owns `supervisor.Run` and `defer`-based cleanup.

**Tech Stack:** Go 1.26, stdlib only for the leaves (`runtime`, `runtime/debug`, `log/slog`, `net/http`, `os`, `os/signal`); `bootstrap` imports its `ops/` siblings. Companion design spec with complete reference implementations: [`docs/superpowers/specs/2026-07-07-ops-runtime-bootstrap-bundle-design.md`](../specs/2026-07-07-ops-runtime-bootstrap-bundle-design.md).

## Global Constraints

- **Module:** `github.com/dmitrymomot/forge`; Go 1.26 (`new(expr)` allowed, no `ptr.To` wrappers).
- **Black-box tests ONLY** — every test file is `package <pkg>_test`.
- **Options pattern, never builders** — `type Option func(*config)`.
- **`errors.Is`-matchable sentinels** where a package returns errors (none of the leaves define new sentinels; they either don't error or return stdlib/loader errors).
- **`doc.go` per package** with a runnable `Example`.
- **Reference implementations are authoritative in the spec.** Each implementation step names the spec section (e.g. "spec §1a") whose code block you reproduce **verbatim**. Read that section before implementing. Do not paraphrase.
- **Formatting:** after editing a package run `just fmt ./ops/<pkg>/...` (package-path form — the single-file form trips a spurious betteralign error).
- **Linting:** run `just lint` once at the end of the bundle; fix everything it reports.
- **Commits:** conventional-commit subjects; **no Claude attribution / co-author trailers** in any commit message.
- Tasks 1–4 are independent leaves and may be implemented in any order or in parallel. Task 5 (`bootstrap`) depends on all four.

## File Structure

```
ops/buildinfo/buildinfo.go       Info value type + Read/String/LogValue/Handler + ldflags vars
ops/buildinfo/buildinfo_test.go  black-box tests
ops/buildinfo/doc.go             package doc + runnable Example

ops/automaxprocs/automaxprocs.go config/options (+WithCgroupRoot), Set, applyCPU, applyMemory
ops/automaxprocs/cgroup.go       cpuQuota(root)/memoryLimit(root) + pure parseCPUMax/parseMemMax/validMem + readInt
ops/automaxprocs/automaxprocs_test.go  black-box tests via WithCgroupRoot fixtures
ops/automaxprocs/doc.go          package doc

ops/logredact/logredact.go       config/options + slog.Handler (Enabled/Handle/WithAttrs/WithGroup) + redact
ops/logredact/logredact_test.go  black-box tests
ops/logredact/doc.go             package doc + runnable Example

ops/supervisor/options.go        + WithContext(parent) ContextOption; contextConfig.parent field (MODIFY)
ops/supervisor/context.go        NewContext resolves parent, default context.Background (MODIFY)
ops/supervisor/context_withcontext_test.go  black-box tests for the addition

ops/bootstrap/bootstrap.go       Func, ConfigFunc[T], options, Run, RunWithConfig[T], run, buildLogger, loadConfig
ops/bootstrap/bootstrap_test.go  black-box tests
ops/bootstrap/doc.go             package doc + canonical RunWithConfig main() Example
```

---

### Task 1: `ops/buildinfo`

**Files:**
- Create: `ops/buildinfo/buildinfo.go`
- Create: `ops/buildinfo/doc.go`
- Test: `ops/buildinfo/buildinfo_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces:
  - `type Info struct { Version, Commit, BuildTime, GoVersion string; Dirty bool }` (JSON tags per spec §1a)
  - `func Read() Info`
  - `func (Info) String() string`
  - `func (Info) LogValue() slog.Value`
  - `func (Info) Handler() http.Handler`

- [ ] **Step 1: Write the failing test**

Create `ops/buildinfo/buildinfo_test.go`:

```go
package buildinfo_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/ops/buildinfo"
)

func TestReadPopulatesGoVersion(t *testing.T) {
	if buildinfo.Read().GoVersion == "" {
		t.Fatal("Read().GoVersion must be populated from runtime.Version()")
	}
}

func TestInfoString(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   buildinfo.Info
		want string
	}{
		{"empty renders dev", buildinfo.Info{}, "dev"},
		{"version only", buildinfo.Info{Version: "1.2.3"}, "1.2.3"},
		{"version commit time", buildinfo.Info{Version: "1.2.3", Commit: "abcdef1234567890", BuildTime: "2026-07-07T12:00:00Z"}, "1.2.3 (abcdef123456 2026-07-07T12:00:00Z)"},
		{"short commit kept whole", buildinfo.Info{Version: "1.2.3", Commit: "abc1234"}, "1.2.3 (abc1234)"},
		{"dirty flag", buildinfo.Info{Version: "1.2.3", Commit: "abc1234", Dirty: true}, "1.2.3 (abc1234, dirty)"},
		{"dev dirty no meta", buildinfo.Info{Dirty: true}, "dev (dirty)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInfoLogValueGroups(t *testing.T) {
	var buf strings.Builder
	slog.New(slog.NewJSONHandler(&buf, nil)).
		Info("msg", slog.Any("build", buildinfo.Info{Version: "1.2.3", Commit: "abc1234def", GoVersion: "go1.26", Dirty: true}))

	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	grp, ok := rec["build"].(map[string]any)
	if !ok {
		t.Fatalf("build attr is not a group: %T", rec["build"])
	}
	if grp["version"] != "1.2.3" {
		t.Errorf("version = %v", grp["version"])
	}
	if grp["dirty"] != true {
		t.Errorf("dirty = %v", grp["dirty"])
	}
}

func TestInfoLogValueOmitsDirtyWhenFalse(t *testing.T) {
	var buf strings.Builder
	slog.New(slog.NewJSONHandler(&buf, nil)).
		Info("msg", slog.Any("build", buildinfo.Info{Version: "1.2.3"}))
	if strings.Contains(buf.String(), "dirty") {
		t.Errorf("dirty should be omitted when false: %s", buf.String())
	}
}

func TestInfoHandlerServesJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	buildinfo.Info{Version: "1.2.3", Commit: "abc"}.Handler().
		ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/version", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var got buildinfo.Info
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != "1.2.3" || got.Commit != "abc" {
		t.Errorf("decoded = %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ops/buildinfo/`
Expected: FAIL — `package .../buildinfo: no Go files` / undefined `buildinfo.Info`, `Read`.

- [ ] **Step 3: Write the implementation**

Create `ops/buildinfo/buildinfo.go` by reproducing **spec §1a** verbatim (the `Info` type, ldflags vars, `Read`, `versionOrDev`, `shortCommit`, `String`, `LogValue`, `Handler`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ops/buildinfo/`
Expected: PASS (all sub-tests).

- [ ] **Step 5: Write doc.go**

Create `ops/buildinfo/doc.go`: package comment plus a runnable `Example` that prints `buildinfo.Read().String()` shape (per spec §1b). Keep the `Example` output deterministic (assert on a constructed `Info{}`, not `Read()`, if you add `// Output:`).

- [ ] **Step 6: Format**

Run: `just fmt ./ops/buildinfo/...`
Expected: no diff errors.

- [ ] **Step 7: Commit**

```bash
git add ops/buildinfo/
git commit -m "feat(ops/buildinfo): build identity from ldflags + ReadBuildInfo"
```

---

### Task 2: `ops/automaxprocs`

**Files:**
- Create: `ops/automaxprocs/automaxprocs.go`
- Create: `ops/automaxprocs/cgroup.go`
- Create: `ops/automaxprocs/doc.go`
- Test: `ops/automaxprocs/automaxprocs_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces:
  - `func Set(log *slog.Logger, opts ...Option) (undo func())`
  - Options: `WithMemoryHeadroom(float64)`, `WithMinProcs(int)`, `WithCPU(bool)`, `WithMemory(bool)`, `WithCgroupRoot(string)`
  - Unexported (tested via `Set(WithCgroupRoot(tmp))`): `cpuQuota(root)`, `memoryLimit(root)`, `parseCPUMax`, `parseMemMax`, `validMem`.

- [ ] **Step 1: Write the failing test**

Create `ops/automaxprocs/automaxprocs_test.go`:

```go
package automaxprocs_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/dmitrymomot/forge/ops/automaxprocs"
)

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// clearRuntimeEnv blanks GOMAXPROCS/GOMEMLIMIT for the test; Set treats "" as
// unset. t.Setenv restores originals on cleanup.
func clearRuntimeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOMAXPROCS", "")
	t.Setenv("GOMEMLIMIT", "")
}

func writeV2(t *testing.T, cpuMax, memMax string) string {
	t.Helper()
	root := t.TempDir()
	if cpuMax != "" {
		if err := os.WriteFile(filepath.Join(root, "cpu.max"), []byte(cpuMax), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if memMax != "" {
		if err := os.WriteFile(filepath.Join(root, "memory.max"), []byte(memMax), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSetV2CPUQuota(t *testing.T) {
	clearRuntimeEnv(t)
	prev := runtime.GOMAXPROCS(0)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "300000 100000", "")),
		automaxprocs.WithMemory(false))
	if got := runtime.GOMAXPROCS(0); got != 3 {
		t.Errorf("GOMAXPROCS = %d, want 3", got)
	}
	undo()
	if got := runtime.GOMAXPROCS(0); got != prev {
		t.Errorf("undo left GOMAXPROCS = %d, want %d", got, prev)
	}
}

func TestSetV2CPUFloorsToMinOne(t *testing.T) {
	clearRuntimeEnv(t)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "150000 100000", "")), // 1.5 -> floor 1
		automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Errorf("GOMAXPROCS = %d, want 1", got)
	}
}

func TestSetV2CPUUnlimitedLeavesDefault(t *testing.T) {
	clearRuntimeEnv(t)
	prev := runtime.GOMAXPROCS(0)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "max 100000", "")),
		automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != prev {
		t.Errorf("GOMAXPROCS = %d, want unchanged %d", got, prev)
	}
}

func TestSetV1CPUQuota(t *testing.T) {
	clearRuntimeEnv(t)
	root := t.TempDir()
	cpuDir := filepath.Join(root, "cpu")
	if err := os.MkdirAll(cpuDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cpuDir, "cpu.cfs_quota_us"), []byte("400000"), 0o644)
	os.WriteFile(filepath.Join(cpuDir, "cpu.cfs_period_us"), []byte("100000"), 0o644)
	undo := automaxprocs.Set(nopLogger(), automaxprocs.WithCgroupRoot(root), automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != 4 {
		t.Errorf("v1 GOMAXPROCS = %d, want 4", got)
	}
}

func TestSetV2MemoryHeadroom(t *testing.T) {
	clearRuntimeEnv(t)
	prev := debug.SetMemoryLimit(-1)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "", "1000000000")),
		automaxprocs.WithCPU(false),
		automaxprocs.WithMemoryHeadroom(0.9))
	if got := debug.SetMemoryLimit(-1); got != 900000000 {
		t.Errorf("GOMEMLIMIT = %d, want 900000000", got)
	}
	undo()
	if got := debug.SetMemoryLimit(-1); got != prev {
		t.Errorf("undo left GOMEMLIMIT = %d, want %d", got, prev)
	}
}

func TestSetV2MemoryUnlimited(t *testing.T) {
	clearRuntimeEnv(t)
	prev := debug.SetMemoryLimit(-1)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "", "max")),
		automaxprocs.WithCPU(false))
	defer undo()
	if got := debug.SetMemoryLimit(-1); got != prev {
		t.Errorf("GOMEMLIMIT = %d, want unchanged %d", got, prev)
	}
}

func TestSetEnvPresentSkipsCPU(t *testing.T) {
	t.Setenv("GOMAXPROCS", "7") // runtime read env at startup; Set must not override
	t.Setenv("GOMEMLIMIT", "")
	prev := runtime.GOMAXPROCS(0)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(writeV2(t, "300000 100000", "")),
		automaxprocs.WithMemory(false))
	defer undo()
	if got := runtime.GOMAXPROCS(0); got != prev {
		t.Errorf("env present must skip CPU leg; GOMAXPROCS = %d, want %d", got, prev)
	}
}

func TestSetMissingRootIsNoOp(t *testing.T) {
	clearRuntimeEnv(t)
	prevP, prevM := runtime.GOMAXPROCS(0), debug.SetMemoryLimit(-1)
	undo := automaxprocs.Set(nopLogger(),
		automaxprocs.WithCgroupRoot(filepath.Join(t.TempDir(), "nonexistent")))
	defer undo()
	if runtime.GOMAXPROCS(0) != prevP || debug.SetMemoryLimit(-1) != prevM {
		t.Error("missing cgroup root should be a no-op")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ops/automaxprocs/`
Expected: FAIL — undefined `automaxprocs.Set` / `WithCgroupRoot`.

- [ ] **Step 3: Write the implementation**

Create `ops/automaxprocs/automaxprocs.go` from **spec §2a** (config, all options incl. `WithCgroupRoot`, `Set`, `applyCPU`, `applyMemory`) and `ops/automaxprocs/cgroup.go` from **spec §2b** (`cpuQuota(root)`, `parseCPUMax`, `memoryLimit(root)`, `parseMemMax`, `validMem`, `readInt`, `unlimited`). Reproduce both verbatim.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ops/automaxprocs/`
Expected: PASS. (Tests mutate process-global GOMAXPROCS/GOMEMLIMIT and restore via `undo`; they must not use `t.Parallel()`.)

- [ ] **Step 5: Write doc.go**

Create `ops/automaxprocs/doc.go` per spec §2c: package comment noting the cgroup root default (`/sys/fs/cgroup`, overridable via `WithCgroupRoot`), the containerized-use assumption, and the fail-open contract.

- [ ] **Step 6: Format**

Run: `just fmt ./ops/automaxprocs/...`

- [ ] **Step 7: Commit**

```bash
git add ops/automaxprocs/
git commit -m "feat(ops/automaxprocs): set GOMAXPROCS/GOMEMLIMIT from cgroup quotas, fail-open"
```

---

### Task 3: `ops/logredact`

**Files:**
- Create: `ops/logredact/logredact.go`
- Create: `ops/logredact/doc.go`
- Test: `ops/logredact/logredact_test.go`

**Interfaces:**
- Consumes: nothing (stdlib only).
- Produces:
  - `func New(next slog.Handler, opts ...Option) slog.Handler`
  - Options: `WithKeys(...string)`, `WithPaths(...string)`, `WithReplacement(string)`
  - `const DefaultReplacement = "[REDACTED]"`
  - (No level knob — redaction is unconditional.)

- [ ] **Step 1: Write the failing test**

Create `ops/logredact/logredact_test.go`:

```go
package logredact_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/dmitrymomot/forge/ops/logredact"
)

func newLog(buf *bytes.Buffer, opts ...logredact.Option) *slog.Logger {
	return slog.New(logredact.New(slog.NewJSONHandler(buf, nil), opts...))
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	return m
}

func TestRedactTopLevelKey(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).Info("login", "password", "hunter2", "user", "alice")
	m := decode(t, &buf)
	if m["password"] != "[REDACTED]" {
		t.Errorf("password = %v, want [REDACTED]", m["password"])
	}
	if m["user"] != "alice" {
		t.Errorf("user = %v, want alice (untouched)", m["user"])
	}
}

func TestRedactKeyInsideGroupValue(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).
		Info("m", slog.Group("creds", slog.String("password", "hunter2"), slog.String("kind", "basic")))
	creds := decode(t, &buf)["creds"].(map[string]any)
	if creds["password"] != "[REDACTED]" {
		t.Errorf("creds.password = %v", creds["password"])
	}
	if creds["kind"] != "basic" {
		t.Errorf("creds.kind = %v", creds["kind"])
	}
}

func TestRedactKeyUnderWithGroup(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).WithGroup("auth").Info("m", "password", "hunter2")
	auth := decode(t, &buf)["auth"].(map[string]any)
	if auth["password"] != "[REDACTED]" {
		t.Errorf("auth.password = %v", auth["password"])
	}
}

func TestRedactByDottedPath(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithPaths("user.ssn")).Info("m",
		slog.Group("user", slog.String("ssn", "123-45-6789"), slog.String("name", "alice")),
		slog.String("ssn", "top"))
	m := decode(t, &buf)
	if user := m["user"].(map[string]any); user["ssn"] != "[REDACTED]" {
		t.Errorf("user.ssn = %v, want redacted", user["ssn"])
	}
	if m["ssn"] != "top" {
		t.Errorf("top-level ssn = %v, want untouched", m["ssn"])
	}
}

func TestRedactWithAttrsBakedIn(t *testing.T) {
	var buf bytes.Buffer
	// With(...) routes through WithAttrs, not Handle's record attrs — the
	// regression a naive Handle-only redactor misses.
	newLog(&buf, logredact.WithKeys("token")).With("token", "secret-abc").Info("m")
	if decode(t, &buf)["token"] != "[REDACTED]" {
		t.Errorf("WithAttrs-baked token not redacted: %s", buf.String())
	}
}

func TestCustomReplacement(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password"), logredact.WithReplacement("***")).Info("m", "password", "x")
	if got := decode(t, &buf)["password"]; got != "***" {
		t.Errorf("replacement = %v, want ***", got)
	}
}

func TestNonMatchingPassThrough(t *testing.T) {
	var buf bytes.Buffer
	newLog(&buf, logredact.WithKeys("password")).Info("m", "count", 42, "name", "alice")
	m := decode(t, &buf)
	if m["count"] != float64(42) || m["name"] != "alice" {
		t.Errorf("non-matching attrs altered: %+v", m)
	}
}

func TestEnabledDelegates(t *testing.T) {
	var buf bytes.Buffer
	h := logredact.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(Info) should be false (delegates to Warn-level next)")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) should be true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ops/logredact/`
Expected: FAIL — undefined `logredact.New` / `WithKeys`.

- [ ] **Step 3: Write the implementation**

Create `ops/logredact/logredact.go` from **spec §3a** verbatim (config with `keys`/`paths`/`replacement` only — no level fields; `WithKeys`/`WithPaths`/`WithReplacement`; the `handler` with `Enabled`/`Handle` (no level-bypass branch)/`WithAttrs`/`WithGroup`; `redact`, `matches`, `joinPath`; `DefaultReplacement`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ops/logredact/`
Expected: PASS (all sub-tests, especially `TestRedactWithAttrsBakedIn`).

- [ ] **Step 5: Write doc.go**

Create `ops/logredact/doc.go` per spec §3b: package comment + a runnable `Example` wrapping a `slog.NewJSONHandler` and showing a `password` attr emitted as `[REDACTED]`, including one under a `WithGroup`. Use `// Output:` with stable field ordering (a single attr keeps output deterministic).

- [ ] **Step 6: Format**

Run: `just fmt ./ops/logredact/...`

- [ ] **Step 7: Commit**

```bash
git add ops/logredact/
git commit -m "feat(ops/logredact): slog.Handler redacting attrs by key/dotted-path"
```

---

### Task 4: `ops/supervisor` — `WithContext` addition

**Files:**
- Modify: `ops/supervisor/options.go` (add `WithContext`; add `parent` to `contextConfig`)
- Modify: `ops/supervisor/context.go` (`NewContext` resolves parent, default `context.Background()`)
- Test: `ops/supervisor/context_withcontext_test.go`

**Interfaces:**
- Consumes: existing `ContextOption`, `contextConfig`, `NewContext`, `WithForceQuit`.
- Produces: `func WithContext(parent context.Context) ContextOption` — roots the signal context at `parent`; default remains `context.Background()`.

- [ ] **Step 1: Read the current code**

Read `ops/supervisor/context.go` and `ops/supervisor/options.go` to confirm `contextConfig`'s definition site and the exact current `NewContext` body before modifying.

- [ ] **Step 2: Write the failing test**

Create `ops/supervisor/context_withcontext_test.go`:

```go
package supervisor_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

func TestWithContextParentCancels(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, stop := supervisor.NewContext(supervisor.WithContext(parent))
	defer stop()
	select {
	case <-ctx.Done():
		t.Fatal("ctx cancelled before parent")
	default:
	}
	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("ctx not cancelled after parent cancel")
	}
}

func TestWithContextParentCancelsForceQuit(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, stop := supervisor.NewContext(supervisor.WithContext(parent), supervisor.WithForceQuit())
	defer stop()
	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("force-quit ctx not cancelled after parent cancel")
	}
}

func TestNewContextDefaultsToBackground(t *testing.T) {
	ctx, stop := supervisor.NewContext()
	defer stop()
	select {
	case <-ctx.Done():
		t.Fatal("default context should not be cancelled")
	default:
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./ops/supervisor/ -run WithContext`
Expected: FAIL — undefined `supervisor.WithContext`.

- [ ] **Step 4: Write the implementation**

In `ops/supervisor/options.go` add the `parent` field to `contextConfig` and the option (spec §5a):

```go
// WithContext roots the signal context at parent instead of context.Background,
// so cancelling parent triggers the same graceful shutdown a signal would.
// bootstrap uses it to thread main's context; tests use it to shut down without
// sending real signals. The zero/default parent remains context.Background.
func WithContext(parent context.Context) ContextOption {
	return func(c *contextConfig) { c.parent = parent }
}
```

In `ops/supervisor/context.go` replace `NewContext` with the parent-resolving version from spec §5b (resolve `parent := cfg.parent; if parent == nil { parent = context.Background() }`, use `parent` in both the `signal.NotifyContext` branch and the `context.WithCancel` force-quit branch, and add `case <-parent.Done(): return` to the first `select` of the force-quit goroutine).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./ops/supervisor/`
Expected: PASS (new tests plus all existing supervisor tests still green).

- [ ] **Step 6: Format**

Run: `just fmt ./ops/supervisor/...`

- [ ] **Step 7: Commit**

```bash
git add ops/supervisor/
git commit -m "feat(ops/supervisor): WithContext to root the signal context at a parent"
```

---

### Task 5: `ops/bootstrap` (integrator — depends on Tasks 1–4)

**Files:**
- Create: `ops/bootstrap/bootstrap.go`
- Create: `ops/bootstrap/doc.go`
- Test: `ops/bootstrap/bootstrap_test.go`

**Interfaces:**
- Consumes: `buildinfo.Info`/`buildinfo.Read`; `automaxprocs.Set`; `logredact.New`/`WithKeys`; `supervisor.NewContext`/`WithContext`/`WithForceQuit`/`ContextOption`; `logger.New`/`WithConfig`/`Config`; `config.LoadEnv[T]`/`config.Load[T]`/`config.Option`.
- Produces:
  - `type Func func(ctx context.Context, log *slog.Logger) error`
  - `type ConfigFunc[T any] func(ctx context.Context, log *slog.Logger, cfg T) error`
  - `func Run(ctx context.Context, name string, fn Func, opts ...Option) int`
  - `func RunWithConfig[T any](ctx context.Context, name string, fn ConfigFunc[T], opts ...Option) int`
  - Options: `WithLogger(*slog.Logger)`, `WithBuildInfo(buildinfo.Info)`, `WithRedactKeys(...string)`, `WithAutoMaxProcs(bool)`, `WithForceQuit()`, `WithConfigDir(string)`, `WithConfigOptions(...config.Option)`

- [ ] **Step 1: Write the failing test**

Create `ops/bootstrap/bootstrap_test.go`:

```go
package bootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/bootstrap"
	"github.com/dmitrymomot/forge/ops/buildinfo"
	"github.com/dmitrymomot/forge/ops/logger"
)

type testConfig struct {
	Port int    `env:"TEST_PORT"`
	Name string `env:"TEST_NAME"`
}

func TestRunCleanExit(t *testing.T) {
	code := bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, log *slog.Logger) error { return nil },
		bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestRunErrorExitLogs(t *testing.T) {
	var buf bytes.Buffer
	code := bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger) error { return errors.New("boom") },
		bootstrap.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
		bootstrap.WithAutoMaxProcs(false))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("error not logged: %s", buf.String())
	}
}

func TestRunContextCancelIsCleanShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- bootstrap.Run(ctx, "svc",
			func(runCtx context.Context, l *slog.Logger) error {
				<-runCtx.Done()
				return runCtx.Err()
			},
			bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	}()
	cancel() // supervisor.WithContext threads this like a SIGTERM
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d, want 0 (context.Canceled is clean)", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRunWithRedactKeys(t *testing.T) {
	var buf bytes.Buffer
	bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger) error {
			l.Info("login", "password", "hunter2")
			return nil
		},
		bootstrap.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
		bootstrap.WithRedactKeys("password"), bootstrap.WithAutoMaxProcs(false))
	if strings.Contains(buf.String(), "hunter2") {
		t.Errorf("password not redacted: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "[REDACTED]") {
		t.Errorf("expected [REDACTED]: %s", buf.String())
	}
}

func TestRunWithBuildInfoLogs(t *testing.T) {
	var buf bytes.Buffer
	bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger) error { return nil },
		bootstrap.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
		bootstrap.WithBuildInfo(buildinfo.Info{Version: "9.9.9"}),
		bootstrap.WithAutoMaxProcs(false))
	if !strings.Contains(buf.String(), "9.9.9") {
		t.Errorf("build info not logged: %s", buf.String())
	}
}

func TestRunWithConfigLoadsAndPasses(t *testing.T) {
	t.Setenv("TEST_PORT", "8080")
	t.Setenv("TEST_NAME", "svc")
	var got testConfig
	code := bootstrap.RunWithConfig(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger, cfg testConfig) error {
			got = cfg
			return nil
		},
		bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if got.Port != 8080 || got.Name != "svc" {
		t.Errorf("config = %+v, want {8080 svc}", got)
	}
}

func TestRunWithConfigLoadFailureSkipsFn(t *testing.T) {
	t.Setenv("TEST_PORT", "not-a-number") // typeconv int parse fails
	called := false
	code := bootstrap.RunWithConfig(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger, cfg testConfig) error {
			called = true
			return nil
		},
		bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	if code != 1 {
		t.Errorf("exit = %d, want 1 on config load failure", code)
	}
	if called {
		t.Error("fn must not be called when config load fails")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ops/bootstrap/`
Expected: FAIL — undefined `bootstrap.Run` / `RunWithConfig`.

- [ ] **Step 3: Write the implementation**

Create `ops/bootstrap/bootstrap.go` from **spec §4a** verbatim (`Func`, `ConfigFunc[T]`, `options`, all seven `With*` options, `Run`, `RunWithConfig[T]`, the shared `run`, `buildLogger`, `loadConfig[T]`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ops/bootstrap/`
Expected: PASS. If `TestRunWithConfigLoadsAndPasses` fails on env mapping, confirm `config.LoadEnv[T]` reads `env:"..."` tags (it should, via `structfields`); adjust only the test's tag names to match `config`'s convention if needed — do not weaken the assertion.

- [ ] **Step 5: Write doc.go**

Create `ops/bootstrap/doc.go`: package comment + the canonical `main()` `Example` using `RunWithConfig[AppConfig]` (config load → `supervisor.Run` inside the callback, with `defer db.Close()`), per spec §4b. This `Example` is illustrative (no `// Output:` — it references app-specific helpers), so name it `Example` and keep it compile-only by guarding with a `func Example() { ... }` that never runs external I/O, or write it as a doc comment code block if compilation of app helpers is impractical. Prefer a compiling `Example_main` that calls `bootstrap.Run` with a trivial body.

- [ ] **Step 6: Format**

Run: `just fmt ./ops/bootstrap/...`

- [ ] **Step 7: Commit**

```bash
git add ops/bootstrap/
git commit -m "feat(ops/bootstrap): thin runtime bootstrap with Run and generic RunWithConfig"
```

---

### Task 6: Bundle finish — lint + docs sync

**Files:**
- Modify: `docs/packages.md` (post-merge doc sync)

- [ ] **Step 1: Lint the whole tree**

Run: `just lint`
Expected: clean. Fix anything reported (common: `betteralign` struct field order — reorder fields; `modernize` — apply suggested rewrites).

- [ ] **Step 2: Run the full bundle test suite once more**

Run: `go test ./ops/...`
Expected: PASS across `buildinfo`, `automaxprocs`, `logredact`, `supervisor`, `bootstrap`.

- [ ] **Step 3: Update `docs/packages.md`**

Move `buildinfo`, `automaxprocs`, `logredact`, `bootstrap` from `ops/` **planned** to **shipped** (note `bootstrap` replaces the `appmain` roadmap name); drop them from the Wave-1 build-order line; record the `supervisor.WithContext` addition. (Per spec "Post-merge doc sync".)

- [ ] **Step 4: Commit**

```bash
git add docs/packages.md
git commit -m "docs(packages): mark ops runtime-bootstrap bundle shipped"
```

---

## Self-Review

**Spec coverage:**
- §1 `buildinfo` → Task 1 ✓ (Read/String/LogValue/Handler + ldflags).
- §2 `automaxprocs` → Task 2 ✓ (Set, options incl. `WithCgroupRoot`, v1/v2 CPU+memory, env-skip, fail-open).
- §3 `logredact` → Task 3 ✓ (keys/paths/replacement, WithAttrs+WithGroup, no level knob).
- §4 `bootstrap` → Task 5 ✓ (`Run`, `RunWithConfig[T]`, all options, exit codes, redaction, buildinfo, config load+failure, ctx-cancel).
- §5 `supervisor.WithContext` → Task 4 ✓.
- doc.go per package → each task's Step 5 ✓.
- Post-merge `docs/packages.md` sync → Task 6 ✓.

**Placeholder scan:** implementation bodies are referenced to exact spec sections (authoritative verbatim source), test code is complete inline, commands are exact. No `TBD`/`add error handling`/`similar to Task N`.

**Type consistency:** option names, `Func`/`ConfigFunc[T]`, `Run`/`RunWithConfig`, `Set`/`WithCgroupRoot`, `New`/`WithKeys`/`WithPaths`/`WithReplacement`, `WithContext` are used identically in the interfaces, tests, and spec references throughout.
