# Ops Runtime-Bootstrap Bundle — Design Spec

> Date: 2026-07-07 · Status: approved, ready for implementation plan · Ships as one PR.
> Wave 1 (ops glue). Four new `ops/` packages: `buildinfo`, `automaxprocs`,
> `logredact`, `bootstrap`; plus one shipped-package addition
> (`supervisor.WithContext`). Built leaves-first; `bootstrap` composes the rest.

A focused "app runtime bootstrap + log safety" slice. `buildinfo` gives the
process an identity, `automaxprocs` right-sizes the runtime to its cgroup,
`logredact` is a redaction safety-net for logs, and `bootstrap` wires all three
— plus generic config autoload and supervisor's signal context — into a
one-line `main()`. Complete, compilable reference implementations for every
symbol — no sketches. Black-box tests only; options pattern (never builders);
`errors.Is` sentinels where errors exist; minimal deps.

## Locked decisions (from brainstorming)

1. **`bootstrap` is THIN.** Its callback wraps the *whole* app body, so the
   consumer calls `supervisor.Run` itself and uses plain `defer` for cleanup.
   This supersedes two earlier shapes (`[]Service` return, and `[]Option`+
   `Teardown` accumulator): because setup and run share one function scope, Go's
   own `defer` handles teardown (partial-init *and* post-drain), so no framework
   cleanup primitive is needed. `bootstrap` owns only the runtime edges: logger →
   automaxprocs → buildinfo log → config load → signal ctx → exit code. It does
   **not** own the service lifecycle — that stays in `supervisor`.
2. **`bootstrap` delegates the signal context to `supervisor.NewContext`**, via a
   new `supervisor.WithContext(parent)` option that threads the caller's `ctx`
   (so a black-box test cancels `ctx` to trigger graceful shutdown — no real
   signals). This *reverses* an earlier "decouple from supervisor" decision that
   only duplicated `NewContext`'s signal + force-quit logic. `bootstrap` imports
   `supervisor` — normal for the top-level integrator — leaving one owner of
   signal handling.
3. **`bootstrap` builds the logger from `LOG_*` env first** (`WithLogger`
   overrides), wrapping it in `logredact` when `WithRedactKeys` is set — *before*
   loading app config, so a config-load failure can be logged.
4. **`bootstrap` autoloads typed app config, generically.** `Run` carries no
   config; `RunWithConfig[T]` loads a `T` (via `config.LoadEnv[T]` by default, or
   `config.Load[T](dir)` under `WithConfigDir`) and passes it to the callback.
   `T` is inferred from the callback signature. bootstrap's env-logger and the
   app's `T` are independent env reads (harmless); embed `logger.Config` in `T`
   or use `WithLogger` if `T` should drive logging.
5. **`buildinfo` precedence: ldflags win, `ReadBuildInfo` fills gaps.** Explicit
   `-X` link-time values are authoritative; `runtime/debug.ReadBuildInfo`
   supplies VCS revision/time/dirty (from `go build` stamping) and the module
   version (from `go install pkg@version`) when ldflags are absent. Value type,
   not a Service or env-`Config` package.
6. **`automaxprocs` is fail-open and env-respecting.** Any cgroup read/parse
   failure — or running outside a container — is a *logged no-op*, never a
   startup error. An explicit `GOMAXPROCS`/`GOMEMLIMIT` env var wins (that leg is
   skipped). Local `/sys/fs/cgroup` parser, **no uber dependency**. Sets both
   GOMAXPROCS *and* GOMEMLIMIT (uber's lib does only the former).
7. **`logredact` correctly implements the `slog.Handler` contract.** It redacts
   attrs baked in via `WithAttrs` (not only record attrs) and tracks the group
   prefix through `WithGroup` so dotted-path matches work at any nesting depth.

## Resolved knobs

- **`logredact` has no level knob.** A redaction safety-net must be
  unconditional; a level-based bypass leaks secrets in exactly the ERROR logs
  that get shipped (or the DEBUG logs that carry raw bodies). Redaction runs on
  every record. "No redaction in dev" is a wiring choice (don't install the
  handler), not a handler option — re-add a level bypass only on a measured
  throughput need.
- **`automaxprocs` GOMEMLIMIT headroom = 0.9** — the containerized-Go
  convention: a soft GC target below the cgroup hard limit, reserving 10% for
  stacks/non-heap/off-heap. Overridable via `WithMemoryHeadroom`.

## Design invariants

**No global mutable state — one sanctioned exception.**
- No `init()`, no singletons, no registries. `logredact`'s handler,
  `automaxprocs`'s config, and `bootstrap`'s options are all explicitly
  constructed and injected.
- **Exception:** `buildinfo` has three package-level `string` vars
  (`version`, `commit`, `buildTime`). This is unavoidable — `-X` ldflags can
  *only* target package-level string vars. They are written once at link time,
  never mutated at runtime, and read solely by `Read()`. Documented as the
  ldflags contract.

**Every feature works on zero-config defaults.**
- `buildinfo.Read()` — no args; degrades to `ReadBuildInfo`-only, then to
  `"dev"`/empty fields, with no ldflags.
- `automaxprocs.Set(log)` — no options; 0.9 headroom, min 1 proc, both legs on.
- `logredact.New(next, logredact.WithKeys("password"))` — one option to be
  useful; `[REDACTED]` replacement, redact-all-levels by default.
- `bootstrap.Run(ctx, "api", fn)` / `bootstrap.RunWithConfig(ctx, "api", fn)` —
  no options; env logger, automaxprocs on, env config, no buildinfo log, no
  redaction.

**bootstrap imports:** `ops/logger`, `ops/config`, `ops/buildinfo`,
`ops/automaxprocs`, `ops/logredact`, `ops/supervisor` + stdlib. It sits at the
top of the ops dependency DAG — nothing imports it.

---

## Component 1 — `ops/buildinfo`

Value type; free `Read` constructor; `slog.LogValuer` + `fmt.Stringer` +
`http.Handler`. Stdlib only (`runtime`, `runtime/debug`, `log/slog`,
`net/http`, `encoding/json`).

### 1a. `buildinfo.go` — complete

```go
package buildinfo

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
)

// Link-time overrides. Set with, e.g.:
//
//	go build -ldflags "\
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.version=$(git describe --tags --always) \
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.commit=$(git rev-parse --short HEAD) \
//	  -X github.com/dmitrymomot/forge/ops/buildinfo.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// They are the ONLY package-level vars, are written once by the linker, never
// mutated at runtime, and are read solely by Read.
var (
	version   string
	commit    string
	buildTime string
)

// Info is a snapshot of the binary's build identity. The zero value is valid;
// unknown fields are empty (Version reads "dev" via String/LogValue).
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	Dirty     bool   `json:"dirty"`
}

// Read merges link-time ldflags (authoritative when set) over
// runtime/debug.ReadBuildInfo: VCS revision/time/modified from `go build`
// stamping, and the module version from `go install pkg@version`. With neither
// source, fields stay empty and String/LogValue substitute "dev".
func Read() Info {
	i := Info{Version: version, Commit: commit, BuildTime: buildTime, GoVersion: runtime.Version()}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if i.Version == "" && bi.Main.Version != "" {
			i.Version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if i.Commit == "" {
					i.Commit = s.Value
				}
			case "vcs.time":
				if i.BuildTime == "" {
					i.BuildTime = s.Value
				}
			case "vcs.modified":
				if s.Value == "true" {
					i.Dirty = true
				}
			}
		}
	}
	return i
}

func (i Info) versionOrDev() string {
	if i.Version == "" {
		return "dev"
	}
	return i.Version
}

func (i Info) shortCommit() string {
	if len(i.Commit) > 12 {
		return i.Commit[:12]
	}
	return i.Commit
}

// String is a single-line summary: "1.2.3 (abc1234def0 2026-07-07T12:00:00Z, dirty)".
func (i Info) String() string {
	var b strings.Builder
	b.WriteString(i.versionOrDev())
	var parens []string
	if c := i.shortCommit(); c != "" {
		parens = append(parens, c)
	}
	if i.BuildTime != "" {
		parens = append(parens, i.BuildTime)
	}
	if len(parens) > 0 {
		fmt.Fprintf(&b, " (%s", strings.Join(parens, " "))
		if i.Dirty {
			b.WriteString(", dirty")
		}
		b.WriteByte(')')
	} else if i.Dirty {
		b.WriteString(" (dirty)")
	}
	return b.String()
}

// LogValue renders build identity as a grouped slog attribute, so
// log.Info("starting", slog.Any("build", info)) nests version/commit/… .
func (i Info) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("version", i.versionOrDev())}
	if c := i.shortCommit(); c != "" {
		attrs = append(attrs, slog.String("commit", c))
	}
	if i.BuildTime != "" {
		attrs = append(attrs, slog.String("build_time", i.BuildTime))
	}
	if i.GoVersion != "" {
		attrs = append(attrs, slog.String("go", i.GoVersion))
	}
	if i.Dirty {
		attrs = append(attrs, slog.Bool("dirty", true))
	}
	return slog.GroupValue(attrs...)
}

// Handler serves the Info as JSON. Mount at /version behind an auth guard if the
// commit is sensitive.
func (i Info) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(i)
	})
}
```

### 1b. `doc.go` — package comment + runnable `Example` printing `Read().String()`.

---

## Component 2 — `ops/automaxprocs`

Free `Set` func returning an `undo`. Fail-open, env-respecting, stdlib-only
`/sys/fs/cgroup` parser (cgroup v2 primary, v1 fallback). No error return — a
failure is a logged no-op, because a maxprocs read must never break startup.

### 2a. `automaxprocs.go` — complete

```go
package automaxprocs

import (
	"log/slog"
	"os"
	"runtime"
	"runtime/debug"
)

type config struct {
	memHeadroom float64
	minProcs    int
	setCPU      bool
	setMemory   bool
	cgroupRoot  string
}

// Option configures Set.
type Option func(*config)

// WithMemoryHeadroom sets the fraction of the cgroup memory limit used for
// GOMEMLIMIT (default 0.9); the remainder covers stacks, the runtime, and OS
// overhead. Values outside (0,1] are ignored.
func WithMemoryHeadroom(f float64) Option {
	return func(c *config) {
		if f > 0 && f <= 1 {
			c.memHeadroom = f
		}
	}
}

// WithMinProcs floors GOMAXPROCS (default 1); a computed quota below it is
// raised to it. Non-positive ignored.
func WithMinProcs(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.minProcs = n
		}
	}
}

// WithCPU toggles the GOMAXPROCS leg (default true).
func WithCPU(on bool) Option { return func(c *config) { c.setCPU = on } }

// WithMemory toggles the GOMEMLIMIT leg (default true).
func WithMemory(on bool) Option { return func(c *config) { c.setMemory = on } }

// WithCgroupRoot overrides the cgroup mount point (default "/sys/fs/cgroup").
// Its purpose is twofold: real systems that mount the cgroup hierarchy
// elsewhere, and black-box tests, which point it at a temp dir of fixture
// files. An empty dir is ignored.
func WithCgroupRoot(dir string) Option {
	return func(c *config) {
		if dir != "" {
			c.cgroupRoot = dir
		}
	}
}

// Set tunes GOMAXPROCS and GOMEMLIMIT from the process's cgroup CPU and memory
// limits, logging every decision at Info (and no-ops at Debug). It is
// fail-open: a missing/unparbleable cgroup, or a non-Linux host, leaves the
// runtime defaults untouched with a logged no-op. An explicit GOMAXPROCS or
// GOMEMLIMIT environment variable is honored — that leg is skipped so the
// operator's choice wins. The returned undo restores the prior process values
// (for tests or staged shutdown) and is safe to call once.
func Set(log *slog.Logger, opts ...Option) (undo func()) {
	c := config{memHeadroom: 0.9, minProcs: 1, setCPU: true, setMemory: true, cgroupRoot: "/sys/fs/cgroup"}
	for _, o := range opts {
		o(&c)
	}

	prevProcs := runtime.GOMAXPROCS(0)
	prevMem := debug.SetMemoryLimit(-1) // negative reads without setting
	undo = func() {
		runtime.GOMAXPROCS(prevProcs)
		debug.SetMemoryLimit(prevMem)
	}

	if c.setCPU {
		applyCPU(log, c)
	}
	if c.setMemory {
		applyMemory(log, c)
	}
	return undo
}

func applyCPU(log *slog.Logger, c config) {
	if v, ok := os.LookupEnv("GOMAXPROCS"); ok && v != "" {
		log.Debug("automaxprocs: GOMAXPROCS set in env, leaving as-is", slog.String("value", v))
		return
	}
	quota, ok := cpuQuota(c.cgroupRoot)
	if !ok {
		log.Debug("automaxprocs: no cgroup CPU quota, leaving GOMAXPROCS",
			slog.Int("gomaxprocs", runtime.GOMAXPROCS(0)))
		return
	}
	procs := int(quota) // floor
	if procs < c.minProcs {
		procs = c.minProcs
	}
	runtime.GOMAXPROCS(procs)
	log.Info("automaxprocs: set GOMAXPROCS from cgroup CPU quota",
		slog.Int("gomaxprocs", procs), slog.Float64("quota", quota))
}

func applyMemory(log *slog.Logger, c config) {
	if v, ok := os.LookupEnv("GOMEMLIMIT"); ok && v != "" {
		log.Debug("automaxprocs: GOMEMLIMIT set in env, leaving as-is", slog.String("value", v))
		return
	}
	limit, ok := memoryLimit(c.cgroupRoot)
	if !ok {
		log.Debug("automaxprocs: no cgroup memory limit, leaving GOMEMLIMIT")
		return
	}
	target := int64(float64(limit) * c.memHeadroom)
	if target <= 0 {
		return
	}
	debug.SetMemoryLimit(target)
	log.Info("automaxprocs: set GOMEMLIMIT from cgroup memory limit",
		slog.Int64("gomemlimit_bytes", target),
		slog.Int64("cgroup_limit_bytes", limit),
		slog.Float64("headroom", c.memHeadroom))
}
```

### 2b. `cgroup.go` — complete (v2 primary, v1 fallback)

```go
package automaxprocs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// unlimited treats cgroup v1's PAGE_COUNTER_MAX-ish sentinels as "no limit".
const unlimited = int64(1) << 62

// cpuQuota returns cores = quota/period from the cgroup under root, or ok=false
// when unlimited or unreadable. cgroup v2 "cpu.max" is "<quota> <period>" or
// "max <period>"; v1 splits across cpu.cfs_quota_us / cpu.cfs_period_us. The
// file reads stay thin so the pure parser (parseCPUMax) carries the logic;
// full-path coverage rides Set(WithCgroupRoot(tmp)) against fixture files.
func cpuQuota(root string) (cores float64, ok bool) {
	if b, err := os.ReadFile(filepath.Join(root, "cpu.max")); err == nil { // v2
		return parseCPUMax(string(b))
	}
	q, e1 := readInt(filepath.Join(root, "cpu", "cpu.cfs_quota_us")) // v1
	p, e2 := readInt(filepath.Join(root, "cpu", "cpu.cfs_period_us"))
	if e1 == nil && e2 == nil && q > 0 && p > 0 {
		return float64(q) / float64(p), true
	}
	return 0, false
}

// parseCPUMax parses a v2 "cpu.max" line into cores. Pure, hence tested directly.
func parseCPUMax(s string) (cores float64, ok bool) {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) != 2 || f[0] == "max" {
		return 0, false
	}
	q, e1 := strconv.ParseFloat(f[0], 64)
	p, e2 := strconv.ParseFloat(f[1], 64)
	if e1 == nil && e2 == nil && q > 0 && p > 0 {
		return q / p, true
	}
	return 0, false
}

// memoryLimit returns the cgroup memory limit in bytes, or ok=false when
// unlimited or unreadable. v2 "memory.max" is a number or "max"; v1 is
// memory.limit_in_bytes with a huge sentinel meaning unlimited.
func memoryLimit(root string) (bytes int64, ok bool) {
	if b, err := os.ReadFile(filepath.Join(root, "memory.max")); err == nil { // v2
		return parseMemMax(string(b))
	}
	if n, err := readInt(filepath.Join(root, "memory", "memory.limit_in_bytes")); err == nil { // v1
		return validMem(n)
	}
	return 0, false
}

// parseMemMax parses a v2 "memory.max" value ("max" or a byte count). Pure.
func parseMemMax(s string) (bytes int64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "max" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return validMem(n)
}

// validMem accepts a positive, non-sentinel byte count.
func validMem(n int64) (int64, bool) {
	if n > 0 && n < unlimited {
		return n, true
	}
	return 0, false
}

func readInt(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
}
```

### 2c. `doc.go` — package comment noting the cgroup root default
(`/sys/fs/cgroup`, overridable via `WithCgroupRoot`), the containerized-use
assumption, and the fail-open contract.

---

## Component 3 — `ops/logredact`

An `slog.Handler` middleware. Stdlib only (`log/slog`, `context`). The subtlety
is honoring the handler contract: redact at both `Handle` and `WithAttrs`, and
carry the group prefix through `WithGroup` for dotted-path matches.

### 3a. `logredact.go` — complete

```go
package logredact

import (
	"context"
	"log/slog"
)

// DefaultReplacement is substituted for a redacted attribute value.
const DefaultReplacement = "[REDACTED]"

type config struct {
	keys        map[string]struct{}
	paths       map[string]struct{}
	replacement string
}

// Option configures New.
type Option func(*config)

// WithKeys redacts any attribute whose leaf key matches, at any nesting depth.
func WithKeys(keys ...string) Option {
	return func(c *config) {
		for _, k := range keys {
			c.keys[k] = struct{}{}
		}
	}
}

// WithPaths redacts an attribute by its dotted group path, e.g. "user.ssn"
// matches key "ssn" inside group "user" but not a top-level "ssn".
func WithPaths(paths ...string) Option {
	return func(c *config) {
		for _, p := range paths {
			c.paths[p] = struct{}{}
		}
	}
}

// WithReplacement overrides the "[REDACTED]" placeholder.
func WithReplacement(s string) Option { return func(c *config) { c.replacement = s } }

// handler wraps next, redacting matching attribute values before they reach it.
type handler struct {
	next  slog.Handler
	cfg   *config
	group string // dotted prefix of currently-open groups; "" at root
}

// New wraps next so attribute values matching WithKeys / WithPaths are replaced
// before reaching next. It redacts record attrs (Handle), attrs baked in via
// WithAttrs, and nested group values, tracking the group prefix across
// WithGroup so dotted paths resolve at any depth.
func New(next slog.Handler, opts ...Option) slog.Handler {
	c := &config{
		keys:        make(map[string]struct{}),
		paths:       make(map[string]struct{}),
		replacement: DefaultReplacement,
	}
	for _, o := range opts {
		o(c)
	}
	return &handler{next: next, cfg: c}
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redact(h.group, a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.redact(h.group, a) // attrs are baked now — redact eagerly
	}
	return &handler{next: h.next.WithAttrs(red), cfg: h.cfg, group: h.group}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &handler{next: h.next.WithGroup(name), cfg: h.cfg, group: joinPath(h.group, name)}
}

// redact replaces a's value when it matches by key or dotted path; group-valued
// attrs are recursed into. LogValuer values are resolved first so their shape
// is concrete for matching.
func (h *handler) redact(prefix string, a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		sub := a.Value.Group()
		gp := joinPath(prefix, a.Key)
		red := make([]slog.Attr, len(sub))
		for i, s := range sub {
			red[i] = h.redact(gp, s)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(red...)}
	}
	if h.matches(prefix, a.Key) {
		return slog.String(a.Key, h.cfg.replacement)
	}
	return a
}

func (h *handler) matches(prefix, key string) bool {
	if _, ok := h.cfg.keys[key]; ok {
		return true
	}
	if len(h.cfg.paths) > 0 {
		if _, ok := h.cfg.paths[joinPath(prefix, key)]; ok {
			return true
		}
	}
	return false
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
```

### 3b. `doc.go` — package comment + `Example` wrapping a `slog.NewJSONHandler`
and showing a `password` attr emitted as `[REDACTED]`, including one under a
`WithGroup`.

---

## Component 4 — `ops/bootstrap`

The thin integrator. Two entry points share one bootstrap sequence: `Run`
(config-less) and `RunWithConfig[T]` (generic config autoload). Imports the
three siblings + `logger` + `config` + `supervisor`.

### 4a. `bootstrap.go` — complete

```go
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/dmitrymomot/forge/ops/automaxprocs"
	"github.com/dmitrymomot/forge/ops/buildinfo"
	"github.com/dmitrymomot/forge/ops/config"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/ops/logredact"
	"github.com/dmitrymomot/forge/ops/supervisor"
)

// Func is a config-less application body. It receives a context cancelled on
// SIGINT/SIGTERM (or when the ctx passed to Run is cancelled) and the configured
// logger, wires the app, and typically ends by returning supervisor.Run(ctx,…).
// A nil or context.Canceled result is a clean stop; any other error makes Run
// return exit code 1. Use plain defer inside fn for resource teardown — it runs
// after fn returns (i.e. after supervisor.Run drains), and on any early error.
type Func func(ctx context.Context, log *slog.Logger) error

// ConfigFunc is an application body that also receives a loaded, typed config.
// T is inferred from the function literal at the call site.
type ConfigFunc[T any] func(ctx context.Context, log *slog.Logger, cfg T) error

type options struct {
	logger      *slog.Logger
	build       *buildinfo.Info
	redactKeys  []string
	autoMaxProc bool
	forceQuit   bool
	configDir   string
	configOpts  []config.Option
}

// Option configures Run and RunWithConfig.
type Option func(*options)

// WithLogger supplies a prebuilt logger instead of building one from LOG_* env.
// WithRedactKeys still wraps it.
func WithLogger(l *slog.Logger) Option { return func(o *options) { o.logger = l } }

// WithBuildInfo logs the build identity once at startup and is otherwise inert.
func WithBuildInfo(b buildinfo.Info) Option { return func(o *options) { o.build = &b } }

// WithRedactKeys wraps the logger in logredact, redacting these attribute keys.
func WithRedactKeys(keys ...string) Option { return func(o *options) { o.redactKeys = keys } }

// WithAutoMaxProcs toggles the automaxprocs step (default on).
func WithAutoMaxProcs(on bool) Option { return func(o *options) { o.autoMaxProc = on } }

// WithForceQuit makes a second SIGINT/SIGTERM force os.Exit(130) while the first
// drains gracefully — an escape hatch for a hung shutdown. Forwarded to
// supervisor.NewContext.
func WithForceQuit() Option { return func(o *options) { o.forceQuit = true } }

// WithConfigDir switches RunWithConfig from env-only loading to the layered
// YAML+env loader rooted at dir (config.Load). Ignored by Run.
func WithConfigDir(dir string) Option { return func(o *options) { o.configDir = dir } }

// WithConfigOptions forwards options to the underlying config loader (dotenv,
// profile, lookup). Ignored by Run.
func WithConfigOptions(opts ...config.Option) Option {
	return func(o *options) { o.configOpts = opts }
}

// Run bootstraps the process runtime and executes fn (no app config), returning
// a process exit code to pass to os.Exit. In order it: builds the logger from
// LOG_* env (or WithLogger), wrapping it in logredact when WithRedactKeys is set;
// tunes GOMAXPROCS/GOMEMLIMIT via automaxprocs unless WithAutoMaxProcs(false);
// logs build info if WithBuildInfo; derives a SIGINT/SIGTERM-aware context from
// ctx via supervisor.NewContext; then calls fn. bootstrap owns the runtime edges
// only — fn owns the service lifecycle (supervisor.Run) and defer-based cleanup.
func Run(ctx context.Context, name string, fn Func, opts ...Option) int {
	return run(ctx, name, opts, func(runCtx context.Context, log *slog.Logger, _ options) error {
		return fn(runCtx, log)
	})
}

// RunWithConfig is Run plus generic config autoload: it loads a T (env-only via
// config.LoadEnv by default, or the layered YAML+env loader under WithConfigDir)
// and passes it to fn. A load failure is logged and yields exit code 1 without
// calling fn. T is inferred from fn.
func RunWithConfig[T any](ctx context.Context, name string, fn ConfigFunc[T], opts ...Option) int {
	return run(ctx, name, opts, func(runCtx context.Context, log *slog.Logger, o options) error {
		cfg, err := loadConfig[T](o)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		return fn(runCtx, log, cfg)
	})
}

// run holds the shared bootstrap sequence; body wraps the caller's fn (with or
// without config) and receives the resolved options.
func run(ctx context.Context, name string, opts []Option, body func(context.Context, *slog.Logger, options) error) int {
	o := options{autoMaxProc: true}
	for _, opt := range opts {
		opt(&o)
	}

	log, err := buildLogger(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: logger init: %v\n", err)
		return 1
	}
	log = log.With(slog.String("app", name))

	if o.autoMaxProc {
		undo := automaxprocs.Set(log)
		defer undo()
	}
	if o.build != nil {
		log.Info("starting", slog.Any("build", *o.build))
	}

	ctxOpts := []supervisor.ContextOption{supervisor.WithContext(ctx)}
	if o.forceQuit {
		ctxOpts = append(ctxOpts, supervisor.WithForceQuit())
	}
	runCtx, stop := supervisor.NewContext(ctxOpts...)
	defer stop()

	if err := body(runCtx, log, o); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("exit", slog.Any("err", err))
		return 1
	}
	log.Info("stopped")
	return 0
}

func buildLogger(o options) (*slog.Logger, error) {
	base := o.logger
	if base == nil {
		cfg, err := config.LoadEnv[logger.Config]()
		if err != nil {
			return nil, err
		}
		base, err = logger.New(logger.WithConfig(cfg))
		if err != nil {
			return nil, err
		}
	}
	if len(o.redactKeys) > 0 {
		return slog.New(logredact.New(base.Handler(), logredact.WithKeys(o.redactKeys...))), nil
	}
	return base, nil
}

func loadConfig[T any](o options) (T, error) {
	if o.configDir != "" {
		return config.Load[T](o.configDir, o.configOpts...)
	}
	return config.LoadEnv[T](o.configOpts...)
}
```

### 4b. `doc.go` — package comment + the canonical `RunWithConfig[AppConfig]`
`main()` example (config load → `supervisor.Run` inside the callback, with
`defer db.Close()`).

---

## Component 5 — `ops/supervisor` (shipped-package addition)

`bootstrap` reuses supervisor's signal handling instead of duplicating it. The
only gap: `NewContext` hardcodes `context.Background()`, so it can't thread a
caller's context (needed so a black-box test cancels `ctx` to shut down without
real signals). One additive option closes it.

### 5a. `options.go` — new `WithContext` ContextOption

```go
// WithContext roots the signal context at parent instead of context.Background,
// so cancelling parent triggers the same graceful shutdown a signal would.
// bootstrap uses it to thread main's context; tests use it to shut down without
// sending real signals. The zero/default parent remains context.Background.
func WithContext(parent context.Context) ContextOption {
	return func(c *contextConfig) { c.parent = parent }
}
```

`contextConfig` gains a `parent context.Context` field.

### 5b. `context.go` — `NewContext` resolves the parent (both branches)

```go
func NewContext(opts ...ContextOption) (context.Context, context.CancelFunc) {
	var cfg contextConfig
	for _, o := range opts {
		o(&cfg)
	}
	parent := cfg.parent
	if parent == nil {
		parent = context.Background()
	}
	if !cfg.forceQuit {
		return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	}

	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ch:
			cancel()
		case <-parent.Done(): // parent cancelled: nothing to force, just stop watching
			return
		case <-stopped:
			return
		}
		select {
		case <-ch:
			os.Exit(forceQuitCode)
		case <-stopped:
		}
	}()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			signal.Stop(ch)
			close(stopped)
		})
		cancel()
	}
	return ctx, stop
}
```

Backward-compatible: existing `NewContext()` / `NewContext(WithForceQuit())`
callers are unaffected (parent defaults to `context.Background()`).

---

## Testing plan (black-box, `_test` packages)

**buildinfo** (`buildinfo_test`):
- `Read()` with all ldflags empty → non-empty `GoVersion`, `Version` renders
  "dev" via `String`/`LogValue`; JSON round-trips.
- `String()` formats version/short-commit/time/dirty combinations (table).
- `LogValue()` emitted through a `slog` JSON handler nests under a `build` group;
  `dirty` omitted when false.
- `Handler()` responds `200` + `application/json` decodable back into `Info`.
- (ldflags injection isn't black-box-testable without a build step — covered by
  a `//go:build` -tagged example or a Makefile check, noted as out-of-unit.)

**automaxprocs** (`automaxprocs_test`):
- Full path black-box via `Set(log, WithCgroupRoot(tmp))` against fixture files
  written into a temp dir (`t.Setenv` clears `GOMAXPROCS`/`GOMEMLIMIT` first):
  v2 `cpu.max` "300000 100000" → GOMAXPROCS 3; "150000 100000" → 1 (`int(1.5)`
  floors, then min 1); "max 100000" → untouched; v1 quota/period layout;
  memory `max`/number/huge-sentinel → GOMEMLIMIT set at ×headroom or untouched.
  Assert via `runtime.GOMAXPROCS(0)` / `debug.SetMemoryLimit(-1)`, then `undo()`
  restores originals. `WithMinProcs` floor and `WithMemoryHeadroom` scaling
  covered the same way; a leg whose env var is present is skipped; an
  unreadable/garbage root → no-op, no panic.
- `Set` outside a container → no-op, `undo` restores the original GOMAXPROCS.
- `GOMAXPROCS`/`GOMEMLIMIT` env present → that leg skipped (assert value
  unchanged via a captured logger `Recorder`).
- `WithMinProcs` floor applied; `WithMemoryHeadroom` scales the target.
- fail-open: unreadable/garbage cgroup files → no panic, runtime defaults intact.

**logredact** (`logredact_test`) — the correctness-critical suite:
- `WithKeys` redacts a top-level `password`; a same-named key nested in a
  `WithGroup("user")` group; and a key inside a `slog.Group(...)` value.
- `WithPaths("user.ssn")` redacts only the nested `ssn`, leaving a top-level
  `ssn` intact.
- Attrs added via `logger.With(...)` (**WithAttrs path**) are redacted — the
  regression that a naive Handle-only implementation misses.
- `WithReplacement` custom placeholder; non-matching attrs pass through byte-for-
  byte; `LogValuer` values resolved before matching.
- Redaction is unconditional at every level (no bypass knob); `Enabled`
  delegates to next.

**bootstrap** (`bootstrap_test`):
- `Run` with a `fn` returning `nil` → exit `0`; returning an error → exit `1`
  and the error logged (assert via `WithLogger` + `logger.Recorder`).
- `fn` returning `context.Canceled` → exit `0` (clean stop).
- Cancelling the `ctx` passed to `Run` cancels the `runCtx` seen by `fn` — the
  graceful-shutdown-without-signals hook, now via `supervisor.WithContext`.
- `RunWithConfig[T]`: a `T` populated from env is passed to `fn` (`T` inferred
  from the literal); `WithConfigDir` loads via the YAML+env loader; a load
  failure → exit `1`, logged, and `fn` is never called.
- `WithRedactKeys` → an attr logged by `fn` via the injected logger is redacted.
- `WithBuildInfo` logs a `starting` record with a `build` group.
- `WithAutoMaxProcs(false)` → GOMAXPROCS untouched.

**supervisor** (`supervisor_test`, addition):
- `WithContext(parent)`: cancelling `parent` cancels the returned context in
  both plain and `WithForceQuit` modes; with no option the context still roots
  at `context.Background` (existing behavior unchanged).

## File inventory

```
ops/buildinfo/buildinfo.go     Info, Read, String, LogValue, Handler; ldflags vars
ops/buildinfo/doc.go           package doc + Example
ops/automaxprocs/automaxprocs.go  config/options (+WithCgroupRoot), Set, applyCPU, applyMemory
ops/automaxprocs/cgroup.go        cpuQuota(root), memoryLimit(root); pure parseCPUMax/parseMemMax/validMem; readInt
ops/automaxprocs/doc.go           package doc
ops/logredact/logredact.go     config/options, handler (Enabled/Handle/WithAttrs/WithGroup), redact
ops/logredact/doc.go           package doc + Example
ops/bootstrap/bootstrap.go     Func, ConfigFunc[T], options, Run, RunWithConfig[T], run, buildLogger, loadConfig
ops/bootstrap/doc.go           package doc + canonical RunWithConfig main() Example
ops/supervisor/options.go      +WithContext(parent) ContextOption; contextConfig.parent field
ops/supervisor/context.go      NewContext resolves parent (default context.Background)
```

## Non-goals (this PR)

- No `cli` / `diag` / `metrics` / `featureflag` — the rest of the ops-glue wave,
  deliberately deferred; `bootstrap` is the single-entrypoint (serve) path, not a
  command tree.
- No DI container or service registry in `bootstrap`; config loading delegates to
  `ops/config` (bootstrap adds no config framework of its own).
- `bootstrap` doesn't load `.env` implicitly — pass `config.WithDotenv(…)` via
  `WithConfigOptions`.
- No per-process cgroup-path discovery from `/proc/self/cgroup` — reads the
  cgroup root (the containerized case). Refinement is a later, demand-driven fix.
- `logredact` does not partial-mask (e.g. show last 4) — whole-value replacement
  only; `crypto/redact` covers values you own.

## Post-merge doc sync

Move `buildinfo`, `automaxprocs`, `logredact`, `bootstrap` from `ops/`
**planned** to **shipped** in `docs/packages.md` (note `bootstrap` replaces the
`appmain` roadmap name); drop them from the Wave-1 build-order line. Record the
`supervisor.WithContext` addition.
