# ops/logger Async Mode + Sentry Handler Rework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an opt-in async mode to `ops/logger` (`NewAsync` + `CloseFunc` + `WithAsyncBufferSize` + `WithLeveledHandler`) and rework `ops/logger/sentry` from a logger facade into a handler provider (`NewHandler`, delete `New`, slim `Config`).

**Architecture:** Async mode inserts an `asyncHandler` (bounded channel + one worker goroutine + atomic drop counter + stop-channel shutdown) beneath the existing `contextHandler` and above the primary/extra handlers. The sentry package stops building loggers and instead returns a `slog.Handler` (a no-op disabled handler when inactive) for `logger.WithHandler`. Spec: `docs/superpowers/specs/2026-07-15-logger-async-design.md`.

**Tech Stack:** Go 1.26 stdlib only for `ops/logger` (`log/slog`, `sync`, `sync/atomic`, `context`); `ops/logger/sentry` keeps its existing `sentry-go` dependency; `testify` for tests (already used).

## Global Constraints

- `ops/logger` must import ONLY the standard library (there is a deps test asserting this).
- Prefer black-box tests (`package logger_test` / `package sentry_test`); white-box only where an unexported seam requires it (e.g. `newHandlerWith`).
- After changing files run `just fmt ./ops/logger/...` (never single-file fmt — betteralign quirk). After each task finishes run `just lint`.
- Run tests with `just test ./ops/logger/...` (the recipe accepts a path).
- Go 1.26 idioms: `wg.Go(func(){...})` not `wg.Add`/`go`; `for b.Loop()` in benchmarks; `for i := range N` loops.
- Never add Claude attribution to commits. Conventional commit messages. No manual line wrapping in any prose.
- `New` (sync) behavior must not change; all existing tests must stay green untouched except the sentry files explicitly reworked in Tasks 7–8.

---

### Task 1: Refactor `New` to share `buildBase` with the upcoming `NewAsync`

**Files:**
- Modify: `ops/logger/logger.go`

**Interfaces:**
- Produces: `buildBase(c config) (slog.Handler, error)` — resolves level/format/writer, builds the primary handler, wraps extras in `slog.NewMultiHandler`. Returns the handler BENEATH context extraction. Task 4 calls it.
- `New` keeps its exact signature and behavior; this is a pure refactor.

- [ ] **Step 1: Rewrite `New` and extract `buildBase`**

Replace the body of `New` and add `buildBase` in `ops/logger/logger.go` (the `resolveWriter`, `newHandler`, `NewNope` funcs stay as-is):

```go
// New builds an *slog.Logger from the options. With no options it returns a text-format,
// info-level logger writing to os.Stdout. If Config.File is set the primary destination
// becomes that file instead of stdout; the file is opened once and held for the lifetime
// of the process (never closed, like os.Stdout), so call New once at startup rather than
// per request. Handlers added via WithHandler run as parallel destinations beneath
// context extraction. Returns ErrInvalidConfig for bad values and ErrOpenFile if the file
// cannot be opened.
func New(opts ...Option) (*slog.Logger, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}

	base, err := buildBase(c)
	if err != nil {
		return nil, err
	}
	if len(c.extractors) > 0 {
		return slog.New(newContextHandler(base, c.extractors...)), nil
	}
	return slog.New(base), nil
}

// buildBase resolves the writer and builds the handler stack beneath context extraction:
// the primary destination plus any extra parallel handlers. Shared by New and NewAsync.
func buildBase(c config) (slog.Handler, error) {
	level := parseLevel(c.Level)
	if c.levelOverride != nil {
		level = *c.levelOverride
	}
	format := parseFormat(c.Format)
	if c.formatOverride != nil {
		format = *c.formatOverride
	}

	w, err := c.resolveWriter()
	if err != nil {
		return nil, err
	}

	primary := newHandler(format, w, level, c.AddSource)
	if len(c.extraHandlers) > 0 {
		return slog.NewMultiHandler(append([]slog.Handler{primary}, c.extraHandlers...)...), nil
	}
	return primary, nil
}
```

- [ ] **Step 2: Format and run the existing tests (pure refactor — all green, none modified)**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/...`
Expected: PASS (all existing tests).

- [ ] **Step 3: Commit**

```bash
git add ops/logger/logger.go
git commit -m "refactor(logger): extract buildBase from New for reuse by NewAsync"
```

---

### Task 2: `WithLeveledHandler` — per-destination minimum level for extra handlers

**Files:**
- Create: `ops/logger/leveled.go`
- Modify: `ops/logger/options.go` (add `WithLeveledHandler`)
- Test: `ops/logger/options_test.go` (append tests)

**Interfaces:**
- Consumes: `c.extraHandlers` slice in `config` (exists), `ErrInvalidConfig`.
- Produces: `WithLeveledHandler(min slog.Level, h slog.Handler) Option` — public; wraps `h` in the unexported `leveledHandler` and appends to `extraHandlers`. Nil handler accumulates an `ErrInvalidConfig` error.

- [ ] **Step 1: Write the failing tests**

Append to `ops/logger/options_test.go` (it is `package logger_test`; add any missing imports: `io`, `log/slog`, `github.com/dmitrymomot/forge/ops/logger`, testify `assert`/`require`):

```go
func TestWithLeveledHandlerGatesBelowMin(t *testing.T) {
	rl, rec := logger.NewRecorder()
	log, err := logger.New(
		logger.WithOutput(io.Discard),
		logger.WithLevel(slog.LevelDebug),
		logger.WithLeveledHandler(slog.LevelWarn, rl.Handler()),
	)
	require.NoError(t, err)

	log.Info("below")
	log.Warn("at")
	log.Error("above")

	recs := rec.Records()
	require.Len(t, recs, 2)
	assert.Equal(t, "at", recs[0].Message)
	assert.Equal(t, "above", recs[1].Message)
}

func TestWithLeveledHandlerKeepsGateThroughWithAttrsAndGroup(t *testing.T) {
	rl, rec := logger.NewRecorder()
	log, err := logger.New(
		logger.WithOutput(io.Discard),
		logger.WithLevel(slog.LevelDebug),
		logger.WithLeveledHandler(slog.LevelWarn, rl.Handler()),
	)
	require.NoError(t, err)

	derived := log.With("env", "prod").WithGroup("http")
	derived.Info("below") // still gated after WithAttrs/WithGroup
	derived.Warn("kept", "status", 200)

	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "kept", recs[0].Message)
	assert.Equal(t, "prod", recs[0].Attrs["env"])
	assert.Equal(t, int64(200), recs[0].Attrs["http.status"])
}

func TestWithLeveledHandlerNilRejected(t *testing.T) {
	_, err := logger.New(logger.WithLeveledHandler(slog.LevelWarn, nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, logger.ErrInvalidConfig)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./ops/logger/...`
Expected: FAIL — `undefined: logger.WithLeveledHandler`.

- [ ] **Step 3: Implement `leveledHandler` and the option**

Create `ops/logger/leveled.go`:

```go
package logger

import (
	"context"
	"log/slog"
)

// leveledHandler gates a wrapped handler behind its own minimum level, independent of the
// primary destination's level. slog.MultiHandler consults each child's Enabled per record,
// so gating in Enabled is sufficient; Handle trusts that contract.
type leveledHandler struct {
	next slog.Handler
	min  slog.Level
}

func (h *leveledHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min && h.next.Enabled(ctx, level)
}

func (h *leveledHandler) Handle(ctx context.Context, rec slog.Record) error {
	return h.next.Handle(ctx, rec)
}

func (h *leveledHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &leveledHandler{next: h.next.WithAttrs(attrs), min: h.min}
}

func (h *leveledHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &leveledHandler{next: h.next.WithGroup(name), min: h.min}
}
```

Append to `ops/logger/options.go` (after `WithHandler`):

```go
// WithLeveledHandler adds an extra parallel destination that only receives records at min
// and above, independent of the primary destination's level. A nil handler is rejected.
// Valid for both New and NewAsync.
func WithLeveledHandler(min slog.Level, h slog.Handler) Option {
	return func(c *config) {
		if h == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLeveledHandler received a nil slog.Handler", ErrInvalidConfig))
			return
		}
		c.extraHandlers = append(c.extraHandlers, &leveledHandler{next: h, min: min})
	}
}
```

- [ ] **Step 4: Format, run tests to verify they pass**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/logger/leveled.go ops/logger/options.go ops/logger/options_test.go
git commit -m "feat(logger): WithLeveledHandler for per-destination minimum levels"
```

---

### Task 3: `WithAsyncBufferSize` option; `New` rejects it

**Files:**
- Modify: `ops/logger/options.go` (config field + option)
- Modify: `ops/logger/logger.go` (rejection in `New`)
- Test: `ops/logger/options_test.go` (append tests)

**Interfaces:**
- Produces: `config.asyncBufferSize int` (0 = unset; `NewAsync` in Task 4 reads it, falling back to `defaultAsyncBufferSize = 8192` declared in Task 4's `async.go`), and `WithAsyncBufferSize(n int) Option`.

- [ ] **Step 1: Write the failing tests**

Append to `ops/logger/options_test.go`:

```go
func TestNewRejectsWithAsyncBufferSize(t *testing.T) {
	_, err := logger.New(logger.WithOutput(io.Discard), logger.WithAsyncBufferSize(64))
	require.Error(t, err)
	assert.ErrorIs(t, err, logger.ErrInvalidConfig)
}

func TestWithAsyncBufferSizeRejectsNonPositive(t *testing.T) {
	for _, n := range []int{0, -1} {
		_, err := logger.New(logger.WithOutput(io.Discard), logger.WithAsyncBufferSize(n))
		require.Error(t, err, "n=%d", n)
		assert.ErrorIs(t, err, logger.ErrInvalidConfig)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./ops/logger/...`
Expected: FAIL — `undefined: logger.WithAsyncBufferSize`.

- [ ] **Step 3: Implement**

In `ops/logger/options.go`, add the field to `config` (keep struct field ordering betteralign-friendly — pointers/slices first, then the int, `errs` last is fine as-is; `just fmt` will flag misalignment):

```go
type config struct {
	Config
	outputOverride  io.Writer // WithOutput; nil means use Config.File or stdout
	levelOverride   *slog.Level
	formatOverride  *Format
	extractors      []ContextExtractor
	extraHandlers   []slog.Handler
	errs            []error
	asyncBufferSize int // WithAsyncBufferSize; 0 means unset (NewAsync uses the default)
}
```

Append the option:

```go
// WithAsyncBufferSize sets the async record buffer capacity (default 8192). n < 1 is
// rejected. Only valid with NewAsync; New returns ErrInvalidConfig if it is set.
func WithAsyncBufferSize(n int) Option {
	return func(c *config) {
		if n < 1 {
			c.errs = append(c.errs, fmt.Errorf("%w: WithAsyncBufferSize requires n >= 1, got %d", ErrInvalidConfig, n))
			return
		}
		c.asyncBufferSize = n
	}
}
```

In `ops/logger/logger.go`, inside `New` right after the `c.Validate()` check, add:

```go
	if c.asyncBufferSize != 0 {
		return nil, fmt.Errorf("%w: WithAsyncBufferSize is only valid with NewAsync", ErrInvalidConfig)
	}
```

Add `"fmt"` to `logger.go` imports.

- [ ] **Step 4: Format, run tests to verify they pass**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/logger/options.go ops/logger/logger.go ops/logger/options_test.go
git commit -m "feat(logger): WithAsyncBufferSize option, rejected by sync New"
```

---

### Task 4: Async core — `NewAsync`, `CloseFunc`, happy-path delivery

**Files:**
- Create: `ops/logger/async.go`
- Create: `ops/logger/async_test.go`

**Interfaces:**
- Consumes: `buildBase(c config) (slog.Handler, error)` (Task 1), `config.asyncBufferSize` (Task 3), `newContextHandler` (exists).
- Produces: `NewAsync(opts ...Option) (*slog.Logger, CloseFunc, error)`, `type CloseFunc func(ctx context.Context) error`, unexported `asyncHandler`/`asyncCore` with `newAsyncHandler(base slog.Handler, bufSize int) *asyncHandler` and `(*asyncCore).close(ctx) error`. Tasks 5–6 add tests against these; Task 9 benchmarks them.

- [ ] **Step 1: Write the failing tests**

Create `ops/logger/async_test.go`:

```go
package logger_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
)

// closeCtx returns a context that bounds a drain wait in tests.
func closeCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestAsyncDeliversAllRecordsInOrder(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(&buf),
		logger.WithFormat(logger.FormatJSON),
	)
	require.NoError(t, err)

	for i := range 100 {
		log.Info("msg", "i", i)
	}
	require.NoError(t, closeLog(closeCtx(t)))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 100)
	for i, line := range lines {
		assert.Contains(t, line, fmt.Sprintf(`"i":%d`, i))
	}
}

func TestAsyncPreBoundAttrsAndGroups(t *testing.T) {
	rl, rec := logger.NewRecorder()
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(io.Discard),
		logger.WithHandler(rl.Handler()),
	)
	require.NoError(t, err)

	log.With("env", "prod").WithGroup("http").Info("done", "status", 200)
	require.NoError(t, closeLog(closeCtx(t)))

	recs := rec.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "done", recs[0].Message)
	assert.Equal(t, "prod", recs[0].Attrs["env"])
	assert.Equal(t, int64(200), recs[0].Attrs["http.status"])
}

func TestAsyncBelowLevelProducesNoOutput(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(&buf),
		logger.WithLevel(slog.LevelWarn),
	)
	require.NoError(t, err)

	log.Info("skip")
	require.NoError(t, closeLog(closeCtx(t)))
	assert.Empty(t, buf.String())
}

func TestAsyncConstructionErrors(t *testing.T) {
	log, closeLog, err := logger.NewAsync(logger.WithOutput(nil))
	require.Error(t, err)
	assert.ErrorIs(t, err, logger.ErrInvalidConfig)
	assert.Nil(t, log)
	assert.Nil(t, closeLog)
}

func TestAsyncConcurrentProducers(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(logger.WithOutput(&buf))
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 200 {
				log.Info("concurrent", "i", i)
			}
		})
	}
	wg.Wait()
	require.NoError(t, closeLog(closeCtx(t)))
	// 1600 records < default 8192 buffer, so none can drop even if the worker never ran.
	assert.Equal(t, 1600, strings.Count(buf.String(), "concurrent"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./ops/logger/...`
Expected: FAIL — `undefined: logger.NewAsync`.

- [ ] **Step 3: Implement the async core**

Create `ops/logger/async.go`:

```go
package logger

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// defaultAsyncBufferSize is the record buffer capacity when WithAsyncBufferSize is unset.
const defaultAsyncBufferSize = 8192

// CloseFunc drains the async buffer and stops the worker; ctx bounds the drain wait. It is
// idempotent — every call waits for the same completion — and returns ctx.Err() if the
// drain does not finish in time (the worker keeps draining in the background).
type CloseFunc func(ctx context.Context) error

// NewAsync is New with a buffered, single-worker async core beneath context extraction:
// log calls extract context attributes, clone the record, and enqueue without blocking;
// one worker goroutine formats and writes to every destination. When the buffer is full
// new records are dropped and counted, and the worker reports the tally as a Warn record
// ("logger: dropped log records", dropped=N) once it catches up. Records logged after
// Close are silently dropped; records buffered at crash/os.Exit are lost. The returned
// CloseFunc must be called on shutdown, before flushing any downstream sinks.
func NewAsync(opts ...Option) (*slog.Logger, CloseFunc, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return nil, nil, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}

	base, err := buildBase(c)
	if err != nil {
		return nil, nil, err
	}
	bufSize := c.asyncBufferSize
	if bufSize == 0 {
		bufSize = defaultAsyncBufferSize
	}
	ah := newAsyncHandler(base, bufSize)
	var top slog.Handler = ah
	if len(c.extractors) > 0 {
		top = newContextHandler(top, c.extractors...)
	}
	return slog.New(top), ah.core.close, nil
}

// asyncItem is one queued record bound to the handler that must process it (the base as
// seen through any WithAttrs/WithGroup derivations at enqueue time).
type asyncItem struct {
	ctx    context.Context
	target slog.Handler
	rec    slog.Record
}

// asyncCore is the state shared by an asyncHandler and every handler derived from it via
// WithAttrs/WithGroup: one queue, one worker, one drop counter, one lifecycle.
type asyncCore struct {
	ch      chan asyncItem
	stop    chan struct{}
	done    chan struct{}
	root    slog.Handler // construction-time base; receives drop reports
	dropped atomic.Int64
	closed  atomic.Bool
	once    sync.Once
}

// asyncHandler enqueues records for the shared worker. Enabled delegates to the wrapped
// base, so records below every destination's level are never cloned or enqueued.
type asyncHandler struct {
	core *asyncCore
	base slog.Handler
}

// newAsyncHandler builds the handler and starts the single worker goroutine.
func newAsyncHandler(base slog.Handler, bufSize int) *asyncHandler {
	core := &asyncCore{
		ch:   make(chan asyncItem, bufSize),
		stop: make(chan struct{}),
		done: make(chan struct{}),
		root: base,
	}
	go core.run()
	return &asyncHandler{core: core, base: base}
}

func (h *asyncHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

// Handle clones the record (it shares an attr backing array with the caller) and enqueues
// it without blocking; a full buffer drops the record and counts it. Always returns nil —
// in async mode delivery problems are reported via the drop tally, never to the log call.
func (h *asyncHandler) Handle(ctx context.Context, rec slog.Record) error {
	if h.core.closed.Load() {
		return nil
	}
	select {
	case h.core.ch <- asyncItem{ctx: context.WithoutCancel(ctx), target: h.base, rec: rec.Clone()}:
	default:
		h.core.dropped.Add(1)
	}
	return nil
}

func (h *asyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &asyncHandler{core: h.core, base: h.base.WithAttrs(attrs)}
}

func (h *asyncHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &asyncHandler{core: h.core, base: h.base.WithGroup(name)}
}

// run is the worker loop: process items as they arrive; once stop closes, drain whatever
// remains and exit. Downstream Handle errors are ignored — identical to what slog.Logger
// does with handler errors in sync mode.
func (c *asyncCore) run() {
	defer close(c.done)
	for {
		select {
		case item := <-c.ch:
			c.reportDrops()
			_ = item.target.Handle(item.ctx, item.rec)
		case <-c.stop:
			for {
				select {
				case item := <-c.ch:
					c.reportDrops()
					_ = item.target.Handle(item.ctx, item.rec)
				default:
					c.reportDrops()
					return
				}
			}
		}
	}
}

// reportDrops emits the accumulated drop tally as a Warn record to the construction-time
// base handler, so every destination (gated by its own level) sees drop incidents.
func (c *asyncCore) reportDrops() {
	n := c.dropped.Swap(0)
	if n == 0 {
		return
	}
	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "logger: dropped log records", 0)
	rec.AddAttrs(slog.Int64("dropped", n))
	_ = c.root.Handle(context.Background(), rec)
}

// close implements CloseFunc; NewAsync hands it out as a method value. Records enqueued by
// Handle calls racing with close are either drained or abandoned — both within the
// post-Close silent-drop contract. The data channel is never closed, so a racing send can
// never panic.
func (c *asyncCore) close(ctx context.Context) error {
	c.once.Do(func() {
		c.closed.Store(true)
		close(c.stop)
	})
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

- [ ] **Step 4: Format, run tests (including race) to verify they pass**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/... && go test -race ./ops/logger/...`
Expected: PASS on all.

- [ ] **Step 5: Commit**

```bash
git add ops/logger/async.go ops/logger/async_test.go
git commit -m "feat(logger): NewAsync with bounded buffer, single worker, and CloseFunc"
```

---

### Task 5: Drop-on-full behavior and the drop-tally record

**Files:**
- Modify: `ops/logger/async_test.go` (append gated-writer helper + tests)

**Interfaces:**
- Consumes: `NewAsync`, `WithAsyncBufferSize` from earlier tasks. Pure test task — the Task 4 implementation should already pass; if it does not, fix `async.go` (that is the point of the test).

- [ ] **Step 1: Write the tests**

Append to `ops/logger/async_test.go`:

```go
// gatedWriter blocks every Write until open() is called; entered signals each Write's
// arrival so tests can deterministically wait for the worker to be inside a Write.
type gatedWriter struct {
	gate    chan struct{}
	entered chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func newGatedWriter() *gatedWriter {
	return &gatedWriter{gate: make(chan struct{}), entered: make(chan struct{}, 64)}
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	w.entered <- struct{}{}
	<-w.gate
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *gatedWriter) open() { close(w.gate) }

func (w *gatedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestAsyncDropsOnFullBufferAndReportsTally(t *testing.T) {
	w := newGatedWriter()
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(w),
		logger.WithAsyncBufferSize(1),
	)
	require.NoError(t, err)

	log.Info("first") // worker dequeues it and blocks inside Write
	<-w.entered       // worker is now committed to "first"; the queue is empty
	log.Info("second") // fills the single buffer slot
	log.Info("third")  // dropped
	log.Info("fourth") // dropped

	w.open()
	require.NoError(t, closeLog(closeCtx(t)))

	out := w.String()
	assert.Contains(t, out, "first")
	assert.Contains(t, out, "second")
	assert.NotContains(t, out, "third")
	assert.NotContains(t, out, "fourth")
	assert.Contains(t, out, "logger: dropped log records")
	assert.Contains(t, out, "dropped=2")
}

func TestAsyncDropNeverBlocksCaller(t *testing.T) {
	w := newGatedWriter() // never opened during logging: worker wedged on the first record
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(w),
		logger.WithAsyncBufferSize(1),
	)
	require.NoError(t, err)

	start := time.Now()
	for i := range 10_000 {
		log.Info("flood", "i", i)
	}
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 2*time.Second, "log calls must not block on a wedged sink")

	w.open()
	require.NoError(t, closeLog(closeCtx(t)))
}
```

- [ ] **Step 2: Format, run the tests**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/... && go test -race ./ops/logger/...`
Expected: PASS. If `dropped=2` mismatches, the bug is in enqueue/report logic in `async.go` — fix there, not in the test.

- [ ] **Step 3: Commit**

```bash
git add ops/logger/async_test.go
git commit -m "test(logger): drop-on-full semantics and drop-tally reporting"
```

---

### Task 6: Close semantics — ctx timeout, idempotency, post-Close safety, call-time extraction

**Files:**
- Modify: `ops/logger/async_test.go` (append tests)

**Interfaces:**
- Consumes: `gatedWriter` (Task 5), `NewAsync`, `CloseFunc`.

- [ ] **Step 1: Write the tests**

Append to `ops/logger/async_test.go`:

```go
type asyncCtxKey struct{}

func TestAsyncCloseHonorsContextAndIsIdempotent(t *testing.T) {
	w := newGatedWriter()
	log, closeLog, err := logger.NewAsync(logger.WithOutput(w))
	require.NoError(t, err)

	log.Info("stuck")
	<-w.entered // worker blocked inside Write

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, closeLog(ctx), context.DeadlineExceeded)

	w.open() // release the worker; the background drain completes
	require.NoError(t, closeLog(closeCtx(t))) // second call waits for the same drain
	assert.Contains(t, w.String(), "stuck")
}

func TestAsyncLogAfterCloseIsSilentNoOp(t *testing.T) {
	var buf bytes.Buffer
	log, closeLog, err := logger.NewAsync(logger.WithOutput(&buf))
	require.NoError(t, err)
	require.NoError(t, closeLog(closeCtx(t)))

	log.Info("after close") // must not panic, block, or write
	assert.NotContains(t, buf.String(), "after close")
}

func TestAsyncCloseConcurrentWithLogging(t *testing.T) {
	log, closeLog, err := logger.NewAsync(logger.WithOutput(io.Discard))
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			log.Info("racing", "i", i)
		}
	})
	require.NoError(t, closeLog(closeCtx(t)))
	wg.Wait() // no panic, no race (verified under -race)
}

func TestAsyncExtractorValuesCapturedAtCallTime(t *testing.T) {
	w := newGatedWriter()
	extractor := func(ctx context.Context) (slog.Attr, bool) {
		if v, ok := ctx.Value(asyncCtxKey{}).(string); ok {
			return slog.String("request_id", v), true
		}
		return slog.Attr{}, false
	}
	log, closeLog, err := logger.NewAsync(
		logger.WithOutput(w),
		logger.WithContextExtractors(extractor),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), asyncCtxKey{}, "abc-123"))
	log.InfoContext(ctx, "req done")
	cancel() // the request ends before the worker ever writes

	w.open()
	require.NoError(t, closeLog(closeCtx(t)))
	// Extraction ran on the caller's goroutine at log time, and WithoutCancel kept the
	// canceled request from suppressing the write.
	assert.Contains(t, w.String(), "request_id=abc-123")
}
```

- [ ] **Step 2: Format, run the tests including race**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/... && go test -race ./ops/logger/...`
Expected: PASS.

- [ ] **Step 3: Run `just lint` (logger async work is now functionally complete)**

Run: `just lint`
Expected: clean. Fix any findings in the files this plan created before committing.

- [ ] **Step 4: Commit**

```bash
git add ops/logger/async_test.go
git commit -m "test(logger): async close semantics and call-time context extraction"
```

---

### Task 7: Sentry — add `NewHandler` (handler provider) alongside the old facade

**Files:**
- Modify: `ops/logger/sentry/sentry.go` (add `disabledHandler`, `newHandlerWith`, `NewHandler`; keep `New`/`newWith` for now)
- Create: `ops/logger/sentry/newhandler_test.go` (white-box — the `newHandlerWith` seam is unexported)

**Interfaces:**
- Consumes: `realSentryHandler(cfg Config) (slog.Handler, error)`, `flush`, `noopFlush`, `Flush`, `ErrSentryInit`, `defaultConfig`/`Option` (all exist).
- Produces: `NewHandler(opts ...Option) (slog.Handler, Flush, error)` — public; `newHandlerWith(buildHandler func(Config) (slog.Handler, error), opts ...Option)` — test seam; `disabledHandler{}` — unexported no-op handler whose `Enabled` is always false. Task 8 deletes the old facade.

- [ ] **Step 1: Write the failing tests**

Create `ops/logger/sentry/newhandler_test.go`:

```go
package sentry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allLevels() []slog.Level {
	return []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
}

func TestNewHandlerEmptyDSNReturnsDisabledHandler(t *testing.T) {
	h, fl, err := NewHandler() // DefaultConfig has empty DSN
	require.NoError(t, err)
	require.NotNil(t, h)
	for _, l := range allLevels() {
		assert.False(t, h.Enabled(context.Background(), l), "disabled handler must report Enabled=false at %v", l)
	}
	// The disabled handler is inert but safe through the full slog.Handler surface.
	derived := h.WithAttrs([]slog.Attr{slog.String("k", "v")}).WithGroup("g")
	require.NoError(t, derived.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelError, "x", 0)))
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerInitFailureReturnsDisabledHandlerPlusError(t *testing.T) {
	build := func(Config) (slog.Handler, error) { return nil, errors.New("init boom") }
	cfg := DefaultConfig()
	cfg.DSN = "https://publicKey@o0.ingest.sentry.io/0" // non-empty → build is attempted

	h, fl, err := newHandlerWith(build, WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSentryInit)
	require.NotNil(t, h)
	assert.False(t, h.Enabled(context.Background(), slog.LevelError))
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerInvalidConfigReturnsDisabledHandlerPlusError(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MinLevel = "loud"
	h, fl, err := NewHandler(WithConfig(cfg))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
	require.NotNil(t, h)
	assert.False(t, h.Enabled(context.Background(), slog.LevelError))
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerSuccessReturnsBuiltHandlerAndRealFlush(t *testing.T) {
	fake := slog.NewJSONHandler(io.Discard, nil)
	build := func(Config) (slog.Handler, error) { return fake, nil }
	cfg := DefaultConfig()
	cfg.DSN = "https://publicKey@o0.ingest.sentry.io/0"

	h, fl, err := newHandlerWith(build, WithConfig(cfg))
	require.NoError(t, err)
	assert.Equal(t, fake, h) // the built handler is returned as-is, unwrapped
	require.NotNil(t, fl)
}
```


- [ ] **Step 2: Run tests to verify they fail**

Run: `just test ./ops/logger/...`
Expected: FAIL — `undefined: NewHandler` / `undefined: newHandlerWith`.

- [ ] **Step 3: Implement in `ops/logger/sentry/sentry.go`**

Add above `New` (keep `New`/`newWith` untouched in this task):

```go
// disabledHandler is returned whenever Sentry is inactive (empty DSN, invalid config, or
// init failure). Enabled always reports false, so it is safe — and free — to pass to
// logger.WithHandler unconditionally.
type disabledHandler struct{}

func (disabledHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (disabledHandler) Handle(context.Context, slog.Record) error { return nil }
func (disabledHandler) WithAttrs([]slog.Attr) slog.Handler        { return disabledHandler{} }
func (disabledHandler) WithGroup(string) slog.Handler             { return disabledHandler{} }

// NewHandler builds the Sentry slog.Handler for logger.WithHandler. It ALWAYS returns a
// usable handler and a non-nil Flush: an empty DSN yields a disabled handler and no error;
// an invalid config or SDK init failure yields a disabled handler plus the error, so the
// app keeps logging while the problem is surfaced. Records at Error and above become
// Sentry Issues; MinLevel..error become Sentry Logs when EnableLogs is set. Call NewHandler
// once per process (it initializes the global Sentry hub).
func NewHandler(opts ...Option) (slog.Handler, Flush, error) {
	return newHandlerWith(realSentryHandler, opts...)
}

// newHandlerWith is the test seam: NewHandler passes realSentryHandler; tests pass a fake.
func newHandlerWith(buildHandler func(Config) (slog.Handler, error), opts ...Option) (slog.Handler, Flush, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if len(c.errs) > 0 {
		return disabledHandler{}, noopFlush, errors.Join(c.errs...)
	}
	if err := c.Validate(); err != nil {
		return disabledHandler{}, noopFlush, err
	}
	if c.DSN == "" {
		return disabledHandler{}, noopFlush, nil
	}
	sh, err := buildHandler(c.Config)
	if err != nil {
		return disabledHandler{}, noopFlush, fmt.Errorf("%w: %v", ErrSentryInit, err)
	}
	return sh, flush, nil
}
```

Ensure `sentry.go` imports include `"context"` (new for `disabledHandler`); `errors`, `fmt`, `log/slog` are already imported.

- [ ] **Step 4: Format, run tests to verify they pass**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/...`
Expected: PASS (old facade tests still green alongside the new ones).

- [ ] **Step 5: Commit**

```bash
git add ops/logger/sentry/sentry.go ops/logger/sentry/newhandler_test.go
git commit -m "feat(logger/sentry): NewHandler provider with always-usable disabled fallback"
```

---

### Task 8: Sentry — delete the facade, slim Config and options, rewrite affected tests and docs

**Files:**
- Modify: `ops/logger/sentry/sentry.go` (delete `New`, `newWith`)
- Modify: `ops/logger/sentry/config.go` (drop embedded `logger.Config`, add own `AddSource`)
- Modify: `ops/logger/sentry/options.go` (drop `WithOutput`, `WithContextExtractors`)
- Modify: `ops/logger/sentry/sentry_test.go` (rewrite: composition test)
- Delete: `ops/logger/sentry/sentry_internal_test.go` (its scenarios now live in `newhandler_test.go`)
- Modify: `ops/logger/sentry/config_test.go` (drop embedded-config assertions)
- Modify: `ops/logger/sentry/doc.go` (rewrite for the provider shape)

**Interfaces:**
- Consumes: `NewHandler`/`newHandlerWith`/`disabledHandler` (Task 7).
- Produces: final sentry surface — `Config{DSN, Environment, MinLevel, EnableLogs, AddSource}`, `DefaultConfig`, `Validate`, `Option`, `WithConfig`, `NewHandler`, `Flush`, sentinel errors. `sentry.New`, `sentry.WithOutput`, `sentry.WithContextExtractors` no longer exist (breaking, no repo-internal consumers).

- [ ] **Step 1: Slim `config.go`**

Replace `ops/logger/sentry/config.go` entirely:

```go
package sentry

import (
	"fmt"
	"log/slog"
	"strings"
)

// Config carries the Sentry-specific settings. The env struct tags are inert strings —
// this package imports no config loader. Logger settings live in logger.Config; the two
// env blocks (LOG_*, SENTRY_*) load independently.
type Config struct {
	DSN         string `env:"SENTRY_DSN"`
	Environment string `env:"SENTRY_ENVIRONMENT"`
	// MinLevel is the lowest level forwarded to Sentry Logs, independent of any logger
	// destination's level. "debug"|"info"|"warn"/"warning"|"error".
	MinLevel string `env:"SENTRY_MIN_LEVEL"`
	// EnableLogs opts in to Sentry Logs for the MinLevel..error range; Issues for Error
	// and above are reported regardless.
	EnableLogs bool `env:"SENTRY_ENABLE_LOGS"`
	// AddSource includes the source file:line in records sent to Sentry Logs.
	AddSource bool `env:"SENTRY_ADD_SOURCE"`
}

// DefaultConfig returns the optimal defaults and is the single source of truth for them.
func DefaultConfig() Config {
	return Config{
		Environment: "production",
		MinLevel:    "warn",
	}
}

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped error otherwise. An empty DSN is valid (Sentry is then
// disabled in NewHandler).
func (c Config) Validate() error {
	if _, ok := levelByName(c.MinLevel); !ok {
		return fmt.Errorf("%w: unknown MinLevel %q", ErrInvalidConfig, c.MinLevel)
	}
	return nil
}

// levelByName maps a level name to a slog.Level, reporting whether it is known. Mirrors
// logger.levelByName — copied, not coupled, per CLAUDE.md (no cross-package level helper).
func levelByName(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

// parseLevel maps a validated MinLevel name to its slog.Level (assumes Validate passed).
func parseLevel(s string) slog.Level {
	lvl, _ := levelByName(s)
	return lvl
}
```

- [ ] **Step 2: Slim `options.go`**

Replace `ops/logger/sentry/options.go` entirely:

```go
package sentry

// Option configures NewHandler. Invalid values accumulate and are returned by NewHandler.
type Option func(*config)

// config holds resolved settings for a single NewHandler call. The embedded Config
// carries serializable data; errs collects invalid option values.
type config struct {
	errs []error
	Config
}

func defaultConfig() config {
	return config{Config: DefaultConfig()}
}

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig().
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}
```

- [ ] **Step 3: Delete the facade from `sentry.go`**

In `ops/logger/sentry/sentry.go`: delete the `New` and `newWith` functions and their doc comments. Remove the now-unused `github.com/dmitrymomot/forge/ops/logger` import.

- [ ] **Step 4: Delete `sentry_internal_test.go`, rewrite `sentry_test.go` and `config_test.go`**

```bash
git rm ops/logger/sentry/sentry_internal_test.go
```

Replace `ops/logger/sentry/sentry_test.go` entirely (black-box composition test):

```go
package sentry_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/ops/logger/sentry"
)

func TestNewHandlerComposesWithLogger(t *testing.T) {
	h, fl, err := sentry.NewHandler() // empty DSN → disabled handler, no error
	require.NoError(t, err)

	var buf bytes.Buffer
	log, err := logger.New(logger.WithOutput(&buf), logger.WithHandler(h))
	require.NoError(t, err)

	log.Info("hello")
	assert.Contains(t, buf.String(), "hello") // primary unaffected by the disabled extra
	require.NoError(t, fl(context.Background()))
}

func TestNewHandlerInvalidConfigFlushSafe(t *testing.T) {
	bad := sentry.DefaultConfig()
	bad.MinLevel = "loud"
	_, fl, err := sentry.NewHandler(sentry.WithConfig(bad))
	require.Error(t, err)
	assert.ErrorIs(t, err, sentry.ErrInvalidConfig)
	// Flush is always non-nil, so `defer fl(ctx)` is safe even on a config error.
	require.NotNil(t, fl)
	require.NoError(t, fl(context.Background()))
}
```

Replace `ops/logger/sentry/config_test.go` entirely:

```go
package sentry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/logger/sentry"
)

func TestDefaultConfig(t *testing.T) {
	c := sentry.DefaultConfig()
	assert.Equal(t, "production", c.Environment)
	assert.Equal(t, "warn", c.MinLevel)
	assert.Empty(t, c.DSN)
	assert.False(t, c.EnableLogs)
	assert.False(t, c.AddSource)
}

func TestValidateGoodAndWarningAlias(t *testing.T) {
	require.NoError(t, sentry.DefaultConfig().Validate())
	c := sentry.DefaultConfig()
	c.MinLevel = "WARNING"
	require.NoError(t, c.Validate())
}

func TestValidateBadMinLevel(t *testing.T) {
	c := sentry.DefaultConfig()
	c.MinLevel = "loud"
	err := c.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, sentry.ErrInvalidConfig)
}
```

- [ ] **Step 5: Rewrite `ops/logger/sentry/doc.go`**

```go
// Package sentry provides a Sentry slog.Handler for logger.WithHandler, keeping the
// Sentry SDK out of the core logger's import graph.
//
// # Usage
//
//	sentryHandler, flush, err := sentry.NewHandler(sentry.WithConfig(sentry.Config{
//		DSN:      os.Getenv("SENTRY_DSN"), // empty → disabled handler, no error
//		MinLevel: "warn",
//	}))
//	if err != nil {
//		// non-fatal: sentryHandler is disabled but safe to use — keep going
//	}
//	log, err := logger.New(logger.WithHandler(sentryHandler)) // or logger.NewAsync
//	defer flush(context.Background()) // ships buffered events; no-op when inactive
//
// Error-level (and above) records are reported to Sentry as Issues via
// sentry.CaptureException; the MinLevel..error range is sent as Sentry Logs when
// EnableLogs is set. (The SDK's deprecated log-to-event conversion is not used.)
//
// NewHandler ALWAYS returns a usable handler and a non-nil Flush: an empty DSN yields a
// disabled handler (Enabled reports false) and no error; an invalid config or SDK init
// failure yields a disabled handler plus the error. Sentry being down never takes logging
// down with it. With logger.NewAsync, call the logger's CloseFunc before flush so buffered
// records reach the handler before events ship. Call NewHandler once per process (it
// initializes the global Sentry hub).
package sentry
```

- [ ] **Step 6: Fix the stale assertion message in `handler_test.go`**

In `ops/logger/sentry/handler_test.go`, `TestSentryOptionMapping` still compiles (`AddSource` changed from a promoted to a direct field — same syntax), but its assertion message references the deleted embedded config. Change:

```go
	assert.True(t, opt.AddSource, "AddSource must mirror the embedded logger.Config")
```

to:

```go
	assert.True(t, opt.AddSource, "AddSource must mirror Config.AddSource")
```

`capture_test.go` and `flush_test.go` are untouched (they test `captureHandler` and `flush` — neither referenced the facade or the embedded config).

- [ ] **Step 7: Format, run tests, lint**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/... && just lint`
Expected: all PASS/clean.

- [ ] **Step 8: Commit**

```bash
git add ops/logger/sentry/
git commit -m "feat(logger/sentry)!: replace New facade with NewHandler provider, slim Config"
```

---

### Task 9: Async benchmarks, logger doc.go, post-benchmark pass

**Files:**
- Modify: `ops/logger/perf_bench_test.go` (append async benchmarks)
- Modify: `ops/logger/doc.go` (async + leveled-handler paragraphs)

**Interfaces:**
- Consumes: `NewAsync`, `WithAsyncBufferSize` (white-box — `perf_bench_test.go` is `package logger`).

- [ ] **Step 1: Append benchmarks to `ops/logger/perf_bench_test.go`**

```go
// blockedWriter blocks every Write until unblock is closed — a wedged sink for the drop
// path. After unblock it returns instantly so Close can drain.
type blockedWriter struct{ unblock chan struct{} }

func (w blockedWriter) Write(p []byte) (int, error) {
	<-w.unblock
	return len(p), nil
}

// BenchmarkAsync_HotPath measures the caller-side cost of a log call in async mode:
// context check, record clone, channel send. The worker drains to io.Discard.
func BenchmarkAsync_HotPath(b *testing.B) {
	log, closeLog, err := NewAsync(WithOutput(io.Discard), WithFormat("json"))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		log.Info("hello", "k", "v")
	}
	b.StopTimer()
	_ = closeLog(context.Background())
}

// BenchmarkAsync_DisabledLevel proves below-level calls never clone or enqueue (0 allocs).
func BenchmarkAsync_DisabledLevel(b *testing.B) {
	log, closeLog, err := NewAsync(WithOutput(io.Discard)) // default level: info
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		log.Debug("skip", "k", "v")
	}
	b.StopTimer()
	_ = closeLog(context.Background())
}

// BenchmarkAsync_DropPath measures the caller-side cost when the buffer is full and every
// record is dropped — the never-block guarantee under a wedged sink.
func BenchmarkAsync_DropPath(b *testing.B) {
	w := blockedWriter{unblock: make(chan struct{})}
	log, closeLog, err := NewAsync(WithOutput(w), WithAsyncBufferSize(8))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		log.Info("dropped", "k", "v")
	}
	b.StopTimer()
	close(w.unblock)
	_ = closeLog(context.Background())
}
```

- [ ] **Step 2: Run the benchmarks and record numbers**

Run: `just bench ./ops/logger/...` (or `go test -bench='Async|Baseline' -benchmem -run=^$ ./ops/logger/`)
Expected: `BenchmarkAsync_DisabledLevel` reports 0 allocs/op. Record hot-path vs `BenchmarkBaseline_PlainSlog` numbers — they go in the PR body (repo rule: before/after numbers required).

- [ ] **Step 3: Post-benchmark optimization pass (measured wins only)**

Inspect the hot path (`asyncHandler.Handle`) allocations: expected ~1 alloc (Record.Clone) over the enqueue. If profiles show avoidable allocations (e.g. `context.WithoutCancel` allocating per call), optimize ONLY with a before/after benchmark delta to cite; otherwise change nothing (readable first — docs/design.md §Performance).

- [ ] **Step 4: Update `ops/logger/doc.go`**

Replace the file content with:

```go
// Package logger builds configured *slog.Logger values over the standard library.
//
// New returns a logger with a single primary destination — stdout by default, or a
// local file (created with parent dirs) when Config.File is set; the two are mutually
// exclusive. Optional context extractors inject request-scoped attributes at the
// record's top level on every call. WithHandler attaches extra parallel destinations
// beneath that extraction, and WithLeveledHandler does the same with a per-destination
// minimum level (e.g. stdout at info while a file handler receives only error+).
//
//	log, err := logger.New(
//		logger.WithFormat(logger.FormatJSON),
//		logger.WithContextExtractors(reqIDExtractor),
//	)
//
// NewAsync is New with a buffered async core: log calls never block on the sink — they
// extract context attributes, clone the record, and enqueue; a single worker goroutine
// formats and writes to every destination. When the buffer (default 8192 records,
// WithAsyncBufferSize) is full, new records are dropped, counted, and later reported as
// a Warn record ("logger: dropped log records", dropped=N). The returned CloseFunc
// drains the buffer and must run on shutdown, before flushing downstream sinks; records
// logged after Close are silently dropped, and records buffered at crash/os.Exit are
// lost. Keep the sync New wherever those trade-offs are unacceptable.
//
//	log, closeLog, err := logger.NewAsync(logger.WithFormat(logger.FormatJSON))
//	defer closeLog(ctx)
//
// Serializable settings live in an env-loadable Config (Level, Format, File, AddSource)
// with a DefaultConfig and Validate; writers, handlers, and extractor funcs are
// functional options. New and NewAsync return ErrInvalidConfig for bad option or Config
// values (multiple invalid options are joined together) and ErrOpenFile if the log file
// cannot be opened — these two errors are on separate paths and are never joined to each
// other. A file opened via Config.File is held open for the lifetime of the process and
// never closed (like os.Stdout); no closer is returned for it, so call New once at
// startup rather than per request. NewNope returns a discard logger. The package imports
// only the standard library.
package logger
```

- [ ] **Step 5: Format, full test + lint**

Run: `just fmt ./ops/logger/... && just test ./ops/logger/... && just lint`
Expected: all PASS/clean.

- [ ] **Step 6: Commit**

```bash
git add ops/logger/perf_bench_test.go ops/logger/doc.go
git commit -m "bench(logger): async hot-path/drop-path benchmarks; document async mode"
```

---

### Task 10: Final verification sweep

**Files:** none created — verification only.

- [ ] **Step 1: Full package test run with race detector**

Run: `go test -race -count=2 ./ops/logger/...`
Expected: PASS twice (catches ordering flakes in the gated-writer tests).

- [ ] **Step 2: Full repo check**

Run: `just check`
Expected: fmt clean, lint clean, all tests green repo-wide.

- [ ] **Step 3: Grep for stragglers**

Run: `grep -rn "sentry\.New(" --include="*.go" . ; grep -rn "WithContextExtractors\|WithOutput" --include="*.go" ops/logger/sentry/`
Expected: no `sentry.New(` call sites anywhere; no `WithOutput`/`WithContextExtractors` references left inside the sentry package.

- [ ] **Step 4: Commit any fixups, then hand off to PR flow**

PR body must include: before/after benchmark numbers (Task 9 Step 2), the breaking sentry change (`sentry.New` → `sentry.NewHandler`, slimmed `Config`), and a pointer to the spec. Follow CLAUDE.md PR flow (create PR → wait CI → fix → address Claude review → repeat).
