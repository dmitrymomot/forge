# data/sqlite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `data/sqlite`, a cgo-free SQLite connection factory in the `data/postgres` mold that owns pragma discipline and a reader/writer pool split for max single-node read+write throughput, plus a backward-compatible `WithDialect(SQLite)` addition to `data/migration`.

**Architecture:** `Open` builds two `database/sql` pools over one file on `modernc.org/sqlite` — a pinned single-connection writer (`_txlock=immediate`, WAL) and an N-connection reader (`query_only`, WAL). Pragmas are applied per connection via `_pragma=` DSN params. The package exposes native `*sql.DB` accessors plus convenience routing, tx helpers, error classification, and a structural `Migrator` seam. `data/migration` gains dialect selection so SQLite migrations run through the same package.

**Tech Stack:** Go 1.26, `modernc.org/sqlite` (pure-Go driver, isolated here), `database/sql`, `github.com/pressly/goose/v3` (already a dep of `data/migration`).

## Global Constraints

- Go 1.26; single module `github.com/dmitrymomot/forge`; work ONLY in the current branch `dm/sqlite-package-brainstorm-559219` (never switch).
- Minimal deps: `modernc.org/sqlite` is the one new external module, isolated to `data/sqlite`. No `mattn/go-sqlite3` (cgo). No query builder / ORM.
- Errors: `errors.Is`-matchable single-line sentinels (`ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`); wrap with `%w` + single-line `%v`, no embedded stacks.
- Package anatomy per `docs/design.md`: `doc.go` (runnable example) · `config.go` · `options.go` (`type Option func(*config)`, never builders) · `errors.go` · impl. Two-level path `data/sqlite`.
- Tests black-box (`package sqlite_test`) except where they must assert unexported state (DSN builder → white-box `package sqlite`).
- The writer pool is ALWAYS `MaxOpenConns(1)` and never configurable — that invariant is what makes the model correct.
- After editing Go files run `just fmt ./data/sqlite/...` (and `just fmt ./data/migration/...`); betteralign reorders struct fields, so write fields logically and let `fmt` fix layout. Run `just lint` after the final task.
- Benchmarks required: `bench_test.go` + a post-benchmark optimization pass (measured wins only) with before/after numbers in the PR.
- No Claude/AI attribution in any commit message, PR text, or comment.
- On merge, delete the `data/sqlite` entry from `docs/packages.md`.

---

### Task 1: `data/migration` — add `WithDialect(SQLite)`

Backward-compatible dialect selection so SQLite migrations run through the existing package. Also adds the `modernc.org/sqlite` module (needed by this task's test and every `data/sqlite` task).

**Files:**
- Modify: `data/migration/options.go`
- Modify: `data/migration/migration.go:41-56` (the `Up` method's provider construction)
- Modify: `data/migration/doc.go` (correct the "dialect is fixed to PostgreSQL" line)
- Test: `data/migration/dialect_test.go` (create)
- Modify: `go.mod`, `go.sum` (add `modernc.org/sqlite`)

**Interfaces:**
- Produces: `migration.Dialect` (uint8), constants `migration.Postgres` (zero value, default) and `migration.SQLite`; `migration.WithDialect(d Dialect) Option`.

- [ ] **Step 1: Add the modernc driver dependency**

Run:
```bash
go get modernc.org/sqlite@latest
```
Expected: `go.mod` gains a `modernc.org/sqlite vX.Y.Z` require line; `go.sum` updated. No error.

- [ ] **Step 2: Write the failing test**

Create `data/migration/dialect_test.go`:
```go
package migration_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"

	"github.com/dmitrymomot/forge/data/migration"
)

func TestNew_SQLiteDialect_AppliesMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_init.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nCREATE TABLE probe (id INTEGER PRIMARY KEY);\n"),
		},
	}
	dsn := "file:" + filepath.Join(t.TempDir(), "m.db") + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	m := migration.New(fsys, migration.WithDialect(migration.SQLite))
	if err := m.Up(context.Background(), db); err != nil {
		t.Fatalf("Up: %v", err)
	}

	var name string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='probe'`).Scan(&name)
	if err != nil {
		t.Fatalf("probe table not created: %v", err)
	}
	if name != "probe" {
		t.Fatalf("got table %q, want probe", name)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -run TestNew_SQLiteDialect ./data/migration/`
Expected: build FAILS — `undefined: migration.WithDialect` and `undefined: migration.SQLite`.

- [ ] **Step 4: Add the Dialect type and option**

In `data/migration/options.go`, add the `dialect` field to `config` (place it with the other fields; `fmt` will reorder):
```go
type config struct {
	logger  *slog.Logger
	table   string
	dialect Dialect
}
```
Append to the same file:
```go
// Dialect selects the SQL dialect goose targets.
type Dialect uint8

const (
	// Postgres is the default dialect (goose DialectPostgres).
	Postgres Dialect = iota
	// SQLite targets SQLite (goose DialectSQLite3) — used by data/sqlite.
	SQLite
)

// WithDialect selects the migration dialect. The default is Postgres.
func WithDialect(d Dialect) Option {
	return func(c *config) { c.dialect = d }
}
```

- [ ] **Step 5: Select the goose dialect in Up**

In `data/migration/migration.go`, replace the provider construction (currently `goose.NewProvider(goose.DialectPostgres, db, m.fsys, opts...)`) with:
```go
	dialect := goose.DialectPostgres
	if m.cfg.dialect == SQLite {
		dialect = goose.DialectSQLite3
	}

	provider, err := goose.NewProvider(dialect, db, m.fsys, opts...)
```

- [ ] **Step 6: Fix the doc comments**

In `data/migration/migration.go`, change the `New` comment sentence "The dialect is fixed to PostgreSQL." to "The dialect defaults to PostgreSQL; select another with WithDialect."
In `data/migration/doc.go`, change "The dialect is fixed to PostgreSQL, the framework's declared database." to "The dialect defaults to PostgreSQL, the framework's primary database; WithDialect selects SQLite for data/sqlite." Add `WithDialect` to the Options paragraph.

- [ ] **Step 7: Format, then run the test to verify it passes**

Run:
```bash
just fmt ./data/migration/...
go test -run TestNew_SQLiteDialect ./data/migration/
```
Expected: PASS.

- [ ] **Step 8: Verify existing migration tests still pass (default dialect unchanged)**

Run: `go test ./data/migration/`
Expected: PASS (the Postgres-DSN integration tests self-skip without `FORGE_TEST_POSTGRES_DSN`; dialect and group tests pass).

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum data/migration/
git commit -m "feat(migration): add WithDialect for SQLite migrations"
```

---

### Task 2: `data/sqlite` — errors, Config, Validate

**Files:**
- Create: `data/sqlite/errors.go`
- Create: `data/sqlite/config.go`
- Test: `data/sqlite/config_test.go`

**Interfaces:**
- Produces: `sqlite.Config` (fields per table below), `sqlite.DefaultConfig() Config`, `(Config).Validate() error`; sentinels `ErrInvalidConfig`, `ErrConnect`, `ErrHealthcheck`.

- [ ] **Step 1: Write the sentinels**

Create `data/sqlite/errors.go`:
```go
package sqlite

import "errors"

// Sentinel errors returned (often wrapped) by this package. Match with errors.Is.
//
// These are distinct from the Is… classification predicates (IsUniqueViolation and
// friends), which match the underlying *sqlite.Error result code.
var (
	// ErrInvalidConfig is returned (joined) by Validate and Open when an option or
	// Config field has an invalid value.
	ErrInvalidConfig = errors.New("sqlite: invalid config")
	// ErrConnect is returned by Open when a pool could not be built or the database
	// could not be reached.
	ErrConnect = errors.New("sqlite: connect failed")
	// ErrHealthcheck wraps a failed liveness ping from the Healthcheck closure.
	ErrHealthcheck = errors.New("sqlite: healthcheck failed")
)
```

- [ ] **Step 2: Write the failing Config test**

Create `data/sqlite/config_test.go`:
```go
package sqlite_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func TestDefaultConfig_FailsValidateWithoutPath(t *testing.T) {
	if err := sqlite.DefaultConfig().Validate(); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig for empty Path, got %v", err)
	}
}

func TestValidate_AcceptsMinimalValidConfig(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidate_RejectsBadValues(t *testing.T) {
	base := func() sqlite.Config { c := sqlite.DefaultConfig(); c.Path = "app.db"; return c }
	cases := map[string]func(*sqlite.Config){
		"negative read pool":  func(c *sqlite.Config) { c.ReadPoolSize = -1 },
		"negative mmap":       func(c *sqlite.Config) { c.MmapSize = -1 },
		"negative busy":       func(c *sqlite.Config) { c.BusyTimeout = -time.Second },
		"unknown journal":     func(c *sqlite.Config) { c.JournalMode = "BOGUS" },
		"unknown synchronous": func(c *sqlite.Config) { c.Synchronous = "SOMETIMES" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base()
			mutate(&c)
			if err := c.Validate(); !errors.Is(err, sqlite.ErrInvalidConfig) {
				t.Fatalf("want ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestValidate_AcceptsZeroMmapAndCaseInsensitiveModes(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	cfg.MmapSize = 0
	cfg.JournalMode = "wal"
	cfg.Synchronous = "normal"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -run TestValidate ./data/sqlite/`
Expected: build FAILS — `undefined: sqlite.Config` / `sqlite.DefaultConfig`.

- [ ] **Step 4: Write config.go**

Create `data/sqlite/config.go`:
```go
package sqlite

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config holds the serializable settings for a SQLite database. The env struct tags
// are inert strings — this package imports no config loader. Seed from DefaultConfig
// and overlay an env-parsed copy. Field order is subject to betteralign.
type Config struct {
	Path            string        `env:"SQLITE_PATH"`               // file path (required); ":memory:" special-cased
	JournalMode     string        `env:"SQLITE_JOURNAL_MODE"`       // writer-only pragma; default WAL
	Synchronous     string        `env:"SQLITE_SYNCHRONOUS"`        // default NORMAL (safe+fast under WAL)
	BusyTimeout     time.Duration `env:"SQLITE_BUSY_TIMEOUT"`       // busy_timeout; safety net vs external writers
	ConnMaxIdleTime time.Duration `env:"SQLITE_CONN_MAX_IDLE_TIME"` // reader pool only
	ConnMaxLifetime time.Duration `env:"SQLITE_CONN_MAX_LIFETIME"`  // reader pool only
	MmapSize        int64         `env:"SQLITE_MMAP_SIZE"`          // mmap_size bytes; 0 disables
	CacheSize       int           `env:"SQLITE_CACHE_SIZE"`         // cache_size; negative = KiB
	ReadPoolSize    int           `env:"SQLITE_READ_POOL_SIZE"`     // reader MaxOpenConns; 0 => runtime.NumCPU()
	ForeignKeys     bool          `env:"SQLITE_FOREIGN_KEYS"`       // per-connection foreign_keys pragma
}

// DefaultConfig returns production-sane, throughput-tuned defaults and is the single
// source of truth for them. Path is left empty and must be supplied; DefaultConfig
// alone therefore fails Validate.
func DefaultConfig() Config {
	return Config{
		JournalMode:  "WAL",
		Synchronous:  "NORMAL",
		BusyTimeout:  5 * time.Second,
		ForeignKeys:  true,
		CacheSize:    -16000,    // ~16 MiB (negative = KiB)
		MmapSize:     268435456, // 256 MiB memory-mapped reads
		ReadPoolSize: 0,         // 0 => runtime.NumCPU() at Open
	}
}

var (
	validJournalModes = map[string]bool{"WAL": true, "DELETE": true, "TRUNCATE": true, "PERSIST": true, "MEMORY": true, "OFF": true}
	validSynchronous  = map[string]bool{"OFF": true, "NORMAL": true, "FULL": true, "EXTRA": true}
)

// Validate reports whether the field values are usable, returning an
// ErrInvalidConfig-wrapped, single-line joined error otherwise. Open also calls it
// defensively.
func (c Config) Validate() error {
	var errs []error
	if c.Path == "" {
		errs = append(errs, fmt.Errorf("%w: Path must not be empty", ErrInvalidConfig))
	}
	if c.ReadPoolSize < 0 {
		errs = append(errs, fmt.Errorf("%w: ReadPoolSize must be >= 0", ErrInvalidConfig))
	}
	if c.MmapSize < 0 {
		errs = append(errs, fmt.Errorf("%w: MmapSize must be >= 0", ErrInvalidConfig))
	}
	for _, f := range []struct {
		name string
		d    time.Duration
	}{
		{"BusyTimeout", c.BusyTimeout},
		{"ConnMaxIdleTime", c.ConnMaxIdleTime},
		{"ConnMaxLifetime", c.ConnMaxLifetime},
	} {
		if f.d < 0 {
			errs = append(errs, fmt.Errorf("%w: %s must be >= 0", ErrInvalidConfig, f.name))
		}
	}
	if c.JournalMode != "" && !validJournalModes[strings.ToUpper(c.JournalMode)] {
		errs = append(errs, fmt.Errorf("%w: JournalMode %q is not recognized", ErrInvalidConfig, c.JournalMode))
	}
	if c.Synchronous != "" && !validSynchronous[strings.ToUpper(c.Synchronous)] {
		errs = append(errs, fmt.Errorf("%w: Synchronous %q is not recognized", ErrInvalidConfig, c.Synchronous))
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 5: Format and run tests to verify they pass**

Run:
```bash
just fmt ./data/sqlite/...
go test -run TestValidate ./data/sqlite/ && go test -run TestDefaultConfig ./data/sqlite/
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add data/sqlite/errors.go data/sqlite/config.go data/sqlite/config_test.go
git commit -m "feat(sqlite): add Config, DefaultConfig, Validate, sentinels"
```

---

### Task 3: `data/sqlite` — DSN builder (white-box)

Pure DSN assembly: pragma ordering, in-memory rewrite, `file:` URI escaping. No database opened.

**Files:**
- Create: `data/sqlite/dsn.go`
- Test: `data/sqlite/dsn_test.go` (white-box `package sqlite`)

**Interfaces:**
- Produces (unexported, for later tasks): `type pragma struct{ name, value string }`; `isMemory(path string) bool`; `nextMemName() string`; `buildDSN(cfg Config, extra []pragma, memory bool, memName string, write bool) string`.

- [ ] **Step 1: Write the failing test**

Create `data/sqlite/dsn_test.go`:
```go
package sqlite

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildDSN_WriterHasImmediateWALNoQueryOnly(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = "app.db"
	dsn := buildDSN(cfg, nil, false, "", true)

	for _, want := range []string{
		"file:app.db?",
		"_txlock=immediate",
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(1)",
		"_pragma=temp_store(MEMORY)",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("writer DSN missing %q\n got: %s", want, dsn)
		}
	}
	if strings.Contains(dsn, "query_only") {
		t.Errorf("writer DSN must not set query_only: %s", dsn)
	}
}

func TestBuildDSN_ReaderIsQueryOnlyDeferredNoJournalMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = "app.db"
	dsn := buildDSN(cfg, nil, false, "", false)

	if !strings.Contains(dsn, "_txlock=deferred") {
		t.Errorf("reader DSN must be deferred: %s", dsn)
	}
	if strings.Contains(dsn, "journal_mode") {
		t.Errorf("reader DSN must not set journal_mode: %s", dsn)
	}
	// query_only must be the LAST _pragma entry.
	last := strings.LastIndex(dsn, "_pragma=")
	if !strings.HasPrefix(dsn[last:], "_pragma=query_only(1)") {
		t.Errorf("query_only must be the final pragma: %s", dsn)
	}
}

func TestBuildDSN_MemorySkipsWALUsesSharedCache(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = ":memory:"
	dsn := buildDSN(cfg, nil, true, "memdb-7", true)

	for _, want := range []string{"file:memdb-7", "mode=memory", "cache=shared"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("memory DSN missing %q: %s", want, dsn)
		}
	}
	if strings.Contains(dsn, "journal_mode") {
		t.Errorf("memory DSN must not set journal_mode: %s", dsn)
	}
}

func TestBuildDSN_ExtraPragmaOverrideAppendedAfterBase(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Path = "app.db"
	dsn := buildDSN(cfg, []pragma{{"cache_size", "-2000"}}, false, "", true)
	base := strings.Index(dsn, "cache_size(-16000)")
	over := strings.Index(dsn, "cache_size(-2000)")
	if base < 0 || over < 0 || over < base {
		t.Errorf("override cache_size must appear after the default: %s", dsn)
	}
}

func TestPathToURI_EscapesReservedChars(t *testing.T) {
	got := pathToURI("my data/app?.db")
	if strings.ContainsAny(got, " ?") {
		t.Errorf("path not escaped: %q", got)
	}
	if _, err := url.Parse("file:" + got + "?x=1"); err != nil {
		t.Errorf("escaped path not URL-parseable: %v", err)
	}
}

func TestIsMemory(t *testing.T) {
	for _, p := range []string{":memory:", "file:x?mode=memory&cache=shared"} {
		if !isMemory(p) {
			t.Errorf("isMemory(%q) = false, want true", p)
		}
	}
	if isMemory("/var/db/app.db") {
		t.Errorf("isMemory(file path) = true, want false")
	}
}

func TestNextMemName_Unique(t *testing.T) {
	a, b := nextMemName(), nextMemName()
	if a == b {
		t.Errorf("nextMemName not unique: %q == %q", a, b)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run 'TestBuildDSN|TestPathToURI|TestIsMemory|TestNextMemName' ./data/sqlite/`
Expected: build FAILS — `undefined: buildDSN` etc.

- [ ] **Step 3: Write dsn.go**

Create `data/sqlite/dsn.go`:
```go
package sqlite

import (
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

// pragma is one ordered PRAGMA applied per connection via a _pragma= DSN param.
type pragma struct{ name, value string }

var memCounter atomic.Uint64

// nextMemName returns a process-unique shared-cache in-memory database name.
func nextMemName() string {
	return "memdb-" + strconv.FormatUint(memCounter.Add(1), 10)
}

// isMemory reports whether path requests an in-memory database.
func isMemory(path string) bool {
	return path == ":memory:" || strings.Contains(path, "mode=memory")
}

// boolPragma renders a bool as a SQLite pragma argument.
func boolPragma(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// basePragmas returns the ordered connection pragmas shared by both pools.
// journal_mode is excluded (writer-only) and query_only is added by the reader path.
func basePragmas(cfg Config) []pragma {
	return []pragma{
		{"busy_timeout", strconv.FormatInt(cfg.BusyTimeout.Milliseconds(), 10)},
		{"synchronous", strings.ToUpper(cfg.Synchronous)},
		{"foreign_keys", boolPragma(cfg.ForeignKeys)},
		{"cache_size", strconv.Itoa(cfg.CacheSize)},
		{"mmap_size", strconv.FormatInt(cfg.MmapSize, 10)},
		{"temp_store", "MEMORY"},
	}
}

// buildDSN assembles the modernc DSN for one pool. The writer sets journal_mode(WAL)
// (file DBs only) and _txlock=immediate; the reader omits journal_mode, uses
// _txlock=deferred, and applies query_only(1) as the final pragma. extra holds
// WithPragma additions, applied after the base set (later pragmas win) and before
// query_only on the reader.
func buildDSN(cfg Config, extra []pragma, memory bool, memName string, write bool) string {
	var pragmas []pragma
	if write && !memory && cfg.JournalMode != "" {
		pragmas = append(pragmas, pragma{"journal_mode", strings.ToUpper(cfg.JournalMode)})
	}
	pragmas = append(pragmas, basePragmas(cfg)...)
	pragmas = append(pragmas, extra...)
	if !write {
		pragmas = append(pragmas, pragma{"query_only", "1"}) // must remain last
	}

	params := make([]string, 0, len(pragmas)+3)
	if memory {
		params = append(params, "mode=memory", "cache=shared")
	}
	if write {
		params = append(params, "_txlock=immediate")
	} else {
		params = append(params, "_txlock=deferred")
	}
	for _, p := range pragmas {
		if p.value == "" {
			continue
		}
		params = append(params, "_pragma="+p.name+"("+p.value+")")
	}

	base := "file:" + memName
	if !memory {
		base = "file:" + pathToURI(cfg.Path)
	}
	return base + "?" + strings.Join(params, "&")
}

// pathToURI renders an OS file path as the path portion of a file: URI, percent-
// encoding reserved characters (space, ?, #, …) so the DSN stays parseable.
func pathToURI(p string) string {
	u := url.URL{Path: p}
	return u.String()
}
```

- [ ] **Step 4: Format and run tests to verify they pass**

Run:
```bash
just fmt ./data/sqlite/...
go test -run 'TestBuildDSN|TestPathToURI|TestIsMemory|TestNextMemName' ./data/sqlite/
```
Expected: PASS.

- [ ] **Step 5: Add a fuzz test for DSN parseability**

Append to `data/sqlite/dsn_test.go`:
```go
func FuzzBuildDSN_AlwaysParseable(f *testing.F) {
	f.Add("app.db")
	f.Add("/var/db/my app.db")
	f.Add("weird?#name.db")
	f.Fuzz(func(t *testing.T, path string) {
		if path == "" || isMemory(path) {
			t.Skip()
		}
		cfg := DefaultConfig()
		cfg.Path = path
		dsn := buildDSN(cfg, nil, false, "", true)
		if _, err := url.Parse(dsn); err != nil {
			t.Errorf("unparseable DSN for path %q: %v", path, err)
		}
	})
}
```

- [ ] **Step 6: Run the fuzz test briefly**

Run: `go test -run Fuzz -fuzz FuzzBuildDSN_AlwaysParseable -fuzztime 10s ./data/sqlite/`
Expected: `PASS` with no new corpus failures.

- [ ] **Step 7: Commit**

```bash
git add data/sqlite/dsn.go data/sqlite/dsn_test.go
git commit -m "feat(sqlite): DSN builder with pragma ordering and file URI escaping"
```

---

### Task 4: `data/sqlite` — Open, DB wrapper, options, lifecycle

The core deliverable: two pools, accessors, convenience routing, `Close`, `Healthcheck`, and the `WithConfig`/`WithLogger`/`WithPragma` options.

**Files:**
- Create: `data/sqlite/sqlite.go` (Open + DB type + accessors + routing)
- Create: `data/sqlite/options.go`
- Create: `data/sqlite/lifecycle.go`
- Test: `data/sqlite/sqlite_test.go`
- Test: `data/sqlite/lifecycle_test.go`

**Interfaces:**
- Consumes: `Config`, `Validate`, `buildDSN`, `isMemory`, `nextMemName`, `pragma`, `ErrConnect`, `ErrHealthcheck`, `ErrInvalidConfig`.
- Produces: `type DB struct{...}`; `Open(ctx, ...Option) (*DB, error)`; `(*DB).Writer() *sql.DB`; `(*DB).Reader() *sql.DB`; `(*DB).ExecContext`, `(*DB).QueryContext`, `(*DB).QueryRowContext`, `(*DB).BeginTx`; `Close(*DB, *slog.Logger)`; `Healthcheck(*DB) func(context.Context) error`; options `WithConfig`, `WithLogger`, `WithPragma`; unexported `type config struct{...}` and `type Option func(*config)`.

- [ ] **Step 1: Write the failing tests**

Create `data/sqlite/sqlite_test.go`:
```go
package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func openFile(t *testing.T, opts ...sqlite.Option) *sqlite.DB {
	t.Helper()
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "app.db")
	all := append([]sqlite.Option{sqlite.WithConfig(cfg)}, opts...)
	db, err := sqlite.Open(context.Background(), all...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { sqlite.Close(db, nil) })
	return db
}

func TestOpen_RequiresPath(t *testing.T) {
	if _, err := sqlite.Open(context.Background()); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestOpen_WALAndForeignKeysEnabled(t *testing.T) {
	db := openFile(t)
	var mode string
	if err := db.Writer().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("journal_mode=%q, want wal", mode)
	}
	var fk int
	if err := db.Reader().QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys=%d, want 1", fk)
	}
}

func TestReader_IsQueryOnly(t *testing.T) {
	db := openFile(t)
	if _, err := db.Reader().ExecContext(context.Background(), `CREATE TABLE x(id INTEGER)`); err == nil {
		t.Fatal("write through reader must fail (query_only)")
	}
}

func TestDB_ExecRoutesToWriterQueryToReader(t *testing.T) {
	db := openFile(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t(id) VALUES (42)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var id int
	if err := db.QueryRowContext(ctx, `SELECT id FROM t`).Scan(&id); err != nil {
		t.Fatalf("read via reader: %v", err)
	}
	if id != 42 {
		t.Fatalf("id=%d, want 42", id)
	}
}

func TestConcurrentReadWrite_NoBusy(t *testing.T) {
	db := openFile(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				var n int
				if err := db.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&n); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			if _, err := db.ExecContext(ctx, `INSERT INTO t(id) VALUES (?)`, i); err != nil {
				errCh <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent op failed (busy=%v): %v", sqlite.IsBusy(err), err)
	}
}

func TestMemory_IsolatedBetweenOpens(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = ":memory:"
	ctx := context.Background()
	db1, err := sqlite.Open(ctx, sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer sqlite.Close(db1, nil)
	db2, err := sqlite.Open(ctx, sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer sqlite.Close(db2, nil)

	if _, err := db1.ExecContext(ctx, `CREATE TABLE only1(id INTEGER)`); err != nil {
		t.Fatalf("create in db1: %v", err)
	}
	// Reader in the SAME instance sees it (shared cache).
	if _, err := db1.Reader().ExecContext(ctx, `SELECT 1 FROM only1`); err != nil {
		t.Fatalf("db1 reader cannot see table: %v", err)
	}
	// A different instance must NOT.
	if _, err := db2.Reader().ExecContext(ctx, `SELECT 1 FROM only1`); err == nil {
		t.Fatal("db2 must not see db1's table")
	}
}

func TestWithPragma_Overrides(t *testing.T) {
	db := openFile(t, sqlite.WithPragma("cache_size", "-2000"))
	var n int
	if err := db.Writer().QueryRow(`PRAGMA cache_size`).Scan(&n); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if n != -2000 {
		t.Fatalf("cache_size=%d, want -2000", n)
	}
}

func TestWithLogger_NilRejected(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	if _, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg), sqlite.WithLogger(nil)); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}
```

Create `data/sqlite/lifecycle_test.go`:
```go
package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func TestClose_NilTolerant(t *testing.T) {
	sqlite.Close(nil, nil) // must not panic
}

func TestHealthcheck_OKThenFailAfterClose(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "app.db")
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	check := sqlite.Healthcheck(db)
	if err := check(context.Background()); err != nil {
		t.Fatalf("healthy check failed: %v", err)
	}
	sqlite.Close(db, nil)
	if err := check(context.Background()); !errors.Is(err, sqlite.ErrHealthcheck) {
		t.Fatalf("want ErrHealthcheck after close, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestOpen|TestReader|TestDB_|TestConcurrent|TestMemory|TestWith|TestClose|TestHealthcheck' ./data/sqlite/`
Expected: build FAILS — `undefined: sqlite.Open` / `sqlite.WithConfig` / `sqlite.Close` etc.

- [ ] **Step 3: Write options.go**

Create `data/sqlite/options.go`:
```go
package sqlite

import (
	"fmt"
	"log/slog"
)

// config holds resolved settings for a single Open. The embedded Config carries
// serializable data; the remaining fields are non-serializable code values.
type config struct {
	logger   *slog.Logger
	migrator Migrator
	pragmas  []pragma
	errs     []error
	Config
}

// Option configures Open. Invalid values accumulate and are returned by Open.
type Option func(*config)

// WithConfig sets the whole serializable data block at once. Build the argument from
// DefaultConfig() (or an env-parsed copy); a bare Config{} leaves Path empty (which
// fails Validate). Options apply in order — place WithConfig before code options you
// want layered on top of it.
func WithConfig(cfg Config) Option {
	return func(c *config) { c.Config = cfg }
}

// WithLogger sets the slog.Logger used by Close. Default slog.Default(); a nil logger
// is rejected (ErrInvalidConfig).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithLogger received a nil *slog.Logger", ErrInvalidConfig))
			return
		}
		c.logger = l
	}
}

// WithPragma appends an extra PRAGMA applied per connection to both pools (after the
// Config-derived pragmas, so it overrides them; before the reader's query_only). Use
// it for anything Config does not cover. An empty name is rejected (ErrInvalidConfig).
// Values must be simple pragma tokens (they are not escaped).
func WithPragma(name, value string) Option {
	return func(c *config) {
		if name == "" {
			c.errs = append(c.errs, fmt.Errorf("%w: WithPragma received an empty name", ErrInvalidConfig))
			return
		}
		c.pragmas = append(c.pragmas, pragma{name: name, value: value})
	}
}
```

- [ ] **Step 4: Write sqlite.go**

Create `data/sqlite/sqlite.go`:
```go
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"runtime"

	_ "modernc.org/sqlite" // registers the cgo-free "sqlite" driver
)

// DB wraps the writer and reader connection pools over one SQLite database. Obtain
// the native handles with Writer/Reader, or use the convenience routing methods.
type DB struct {
	writer *sql.DB
	reader *sql.DB
	logger *slog.Logger
}

// Open resolves options, validates, builds the writer (single pinned connection,
// BEGIN IMMEDIATE, WAL) and reader (N connections, query_only) pools, pings each,
// runs the migrator (if any) on the writer, and returns the live *DB. The caller
// owns it and should defer Close(db, logger).
func Open(ctx context.Context, opts ...Option) (*DB, error) {
	cfg := config{Config: DefaultConfig()}
	for _, opt := range opts {
		opt(&cfg)
	}
	if len(cfg.errs) > 0 {
		return nil, errors.Join(cfg.errs...)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	logger := cfg.logger
	if logger == nil {
		logger = slog.Default()
	}

	readConns := cfg.ReadPoolSize
	if readConns <= 0 {
		readConns = runtime.NumCPU()
	}

	memory := isMemory(cfg.Path)
	memName := ""
	if memory {
		memName = nextMemName()
	}

	writer, err := sql.Open("sqlite", buildDSN(cfg.Config, cfg.pragmas, memory, memName, true))
	if err != nil {
		return nil, fmt.Errorf("%w: open writer: %v", ErrConnect, err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxIdleTime(0)
	writer.SetConnMaxLifetime(0)

	reader, err := sql.Open("sqlite", buildDSN(cfg.Config, cfg.pragmas, memory, memName, false))
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("%w: open reader: %v", ErrConnect, err)
	}
	reader.SetMaxOpenConns(readConns)
	reader.SetMaxIdleConns(readConns)
	reader.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	reader.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// Ping the writer first: it creates the file and sets WAL before the query_only
	// reader (which cannot) connects; for memory it creates the shared-cache DB.
	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, fmt.Errorf("%w: ping writer: %v", ErrConnect, err)
	}
	if err := reader.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, fmt.Errorf("%w: ping reader: %v", ErrConnect, err)
	}

	db := &DB{writer: writer, reader: reader, logger: logger}

	if cfg.migrator != nil {
		if err := cfg.migrator.Up(ctx, writer); err != nil {
			_ = writer.Close()
			_ = reader.Close()
			return nil, fmt.Errorf("sqlite: migrate: %w", err)
		}
	}

	return db, nil
}

// Writer returns the single-connection write pool (BEGIN IMMEDIATE, WAL). Send every
// statement that writes here, plus any read that must observe uncommitted writes in a
// write transaction.
func (db *DB) Writer() *sql.DB { return db.writer }

// Reader returns the concurrent read pool (query_only). Send read-only queries here.
func (db *DB) Reader() *sql.DB { return db.reader }

// ExecContext routes to the writer.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.writer.ExecContext(ctx, query, args...)
}

// QueryContext routes to the reader. For a write that returns rows (… RETURNING) use
// Writer().QueryContext instead — the reader is query_only and will reject it.
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.reader.QueryContext(ctx, query, args...)
}

// QueryRowContext routes to the reader (see QueryContext for the RETURNING caveat).
func (db *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return db.reader.QueryRowContext(ctx, query, args...)
}

// BeginTx routes to the writer and acquires the write lock immediately (_txlock).
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return db.writer.BeginTx(ctx, opts)
}
```
Note: add `"errors"` to the import block (used by `errors.Join`); `goimports` via `just fmt` will insert it.

- [ ] **Step 5: Write lifecycle.go**

Create `data/sqlite/lifecycle.go`:
```go
package sqlite

import (
	"context"
	"fmt"
	"log/slog"
)

// Close logs a single line and closes both pools. It is the resource counterpart to
// Open, meant as `defer Close(db, logger)` in main so it runs after the supervisor
// drains every service. A nil db or nil logger is tolerated. It takes no ctx because
// *sql.DB.Close is synchronous.
func Close(db *DB, log *slog.Logger) {
	if db == nil {
		return
	}
	if log != nil {
		log.Info("closing sqlite database")
	}
	if db.reader != nil {
		_ = db.reader.Close()
	}
	if db.writer != nil {
		_ = db.writer.Close()
	}
}

// Healthcheck returns a stateless closure that pings both pools, wrapping any failure
// in ErrHealthcheck. Hand its func(context.Context) error to a readiness probe.
func Healthcheck(db *DB) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := db.writer.PingContext(ctx); err != nil {
			return fmt.Errorf("%w: writer: %v", ErrHealthcheck, err)
		}
		if err := db.reader.PingContext(ctx); err != nil {
			return fmt.Errorf("%w: reader: %v", ErrHealthcheck, err)
		}
		return nil
	}
}
```
Note: this references the `Migrator` type used in `sqlite.go` (`cfg.migrator`). Add a temporary placeholder so the package compiles now — create `data/sqlite/migrator.go` with the interface (it is finalized in Task 7):
```go
package sqlite

import (
	"context"
	"database/sql"
)

// Migrator is the one-method seam between this package and migration. It is
// structural: sqlite does not import migration, and *migration.Migrator satisfies it.
// Up applies pending schema changes against the writer pool's *sql.DB.
type Migrator interface {
	Up(ctx context.Context, db *sql.DB) error
}
```

- [ ] **Step 6: Format, then run the tests to verify they pass**

Run:
```bash
just fmt ./data/sqlite/...
go test -race -run 'TestOpen|TestReader|TestDB_|TestConcurrent|TestMemory|TestWith|TestClose|TestHealthcheck' ./data/sqlite/
```
Expected: PASS (all).

- [ ] **Step 7: Commit**

```bash
git add data/sqlite/sqlite.go data/sqlite/options.go data/sqlite/lifecycle.go data/sqlite/migrator.go data/sqlite/sqlite_test.go data/sqlite/lifecycle_test.go
git commit -m "feat(sqlite): Open with reader/writer pools, accessors, routing, lifecycle"
```

---

### Task 5: `data/sqlite` — error classification

**Files:**
- Create: `data/sqlite/classify.go`
- Test: `data/sqlite/classify_test.go`

**Interfaces:**
- Produces: `IsUniqueViolation(error) bool`, `IsForeignKeyViolation(error) bool`, `IsBusy(error) bool`, `IsNotFound(error) bool`.

- [ ] **Step 1: Write the failing test**

Create `data/sqlite/classify_test.go`:
```go
package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func classifyDB(t *testing.T) *sqlite.DB {
	t.Helper()
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "c.db")
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlite.Close(db, nil) })
	return db
}

func TestIsUniqueViolation(t *testing.T) {
	db := classifyDB(t)
	ctx := context.Background()
	mustExec(t, db, `CREATE TABLE u(id INTEGER PRIMARY KEY, email TEXT UNIQUE)`)
	mustExec(t, db, `INSERT INTO u(id, email) VALUES (1, 'a@x')`)
	_, err := db.ExecContext(ctx, `INSERT INTO u(id, email) VALUES (2, 'a@x')`)
	if !sqlite.IsUniqueViolation(err) {
		t.Fatalf("want unique violation, got %v", err)
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	db := classifyDB(t)
	ctx := context.Background()
	mustExec(t, db, `CREATE TABLE parent(id INTEGER PRIMARY KEY)`)
	mustExec(t, db, `CREATE TABLE child(id INTEGER PRIMARY KEY, pid INTEGER REFERENCES parent(id))`)
	_, err := db.ExecContext(ctx, `INSERT INTO child(id, pid) VALUES (1, 999)`)
	if !sqlite.IsForeignKeyViolation(err) {
		t.Fatalf("want FK violation, got %v", err)
	}
}

func TestIsNotFound(t *testing.T) {
	db := classifyDB(t)
	mustExec(t, db, `CREATE TABLE t(id INTEGER PRIMARY KEY)`)
	var id int
	err := db.QueryRowContext(context.Background(), `SELECT id FROM t WHERE id=1`).Scan(&id)
	if !sqlite.IsNotFound(err) || !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
}

func TestIsBusy_FalseForNonSQLiteErrors(t *testing.T) {
	if sqlite.IsBusy(nil) || sqlite.IsBusy(errors.New("boom")) {
		t.Fatal("IsBusy must be false for nil and non-sqlite errors")
	}
}

func mustExec(t *testing.T, db *sqlite.DB, q string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run 'TestIsUnique|TestIsForeign|TestIsNotFound|TestIsBusy' ./data/sqlite/`
Expected: build FAILS — `undefined: sqlite.IsUniqueViolation` etc.

- [ ] **Step 3: Write classify.go**

Create `data/sqlite/classify.go`:
```go
package sqlite

import (
	"database/sql"
	"errors"

	"modernc.org/sqlite"
)

// SQLite extended result codes recognized by the classification predicates.
const (
	codeBusy                 = 5    // SQLITE_BUSY
	codeLocked               = 6    // SQLITE_LOCKED
	codeConstraintForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	codeConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	codeConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// resultCode extracts the extended result code from err if it (or anything it wraps)
// is a *sqlite.Error; ok is false otherwise.
func resultCode(err error) (code int, ok bool) {
	if e, found := errors.AsType[*sqlite.Error](err); found {
		return e.Code(), true
	}
	return 0, false
}

// IsUniqueViolation reports whether err is a UNIQUE or PRIMARY KEY constraint
// violation. Use it instead of importing the driver at the call site.
func IsUniqueViolation(err error) bool {
	code, ok := resultCode(err)
	return ok && (code == codeConstraintUnique || code == codeConstraintPrimaryKey)
}

// IsForeignKeyViolation reports whether err is a FOREIGN KEY constraint violation.
func IsForeignKeyViolation(err error) bool {
	code, ok := resultCode(err)
	return ok && code == codeConstraintForeignKey
}

// IsBusy reports whether err is a busy/locked condition (SQLITE_BUSY/SQLITE_LOCKED and
// their extended variants, which share the primary result code). This is what
// WithTxRetry retries.
func IsBusy(err error) bool {
	code, ok := resultCode(err)
	if !ok {
		return false
	}
	primary := code & 0xFF
	return primary == codeBusy || primary == codeLocked
}

// IsNotFound reports whether err is sql.ErrNoRows — a missing row, not a failure.
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
```

- [ ] **Step 4: Format and run the tests to verify they pass**

Run:
```bash
just fmt ./data/sqlite/...
go test -race -run 'TestIsUnique|TestIsForeign|TestIsNotFound|TestIsBusy' ./data/sqlite/
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add data/sqlite/classify.go data/sqlite/classify_test.go
git commit -m "feat(sqlite): error classification predicates"
```

---

### Task 6: `data/sqlite` — transactions

**Files:**
- Create: `data/sqlite/tx.go`
- Test: `data/sqlite/tx_test.go`

**Interfaces:**
- Consumes: `DB`, `Config`, `Open`, `buildDSN`, `IsBusy`.
- Produces: `WithTx(ctx, *DB, func(*sql.Tx) error) error`; `WithTxRetry(ctx, *DB, func(*sql.Tx) error, ...RetryOption) error`; `type RetryOption func(*retryConfig)`; `WithRetryAttempts(int) RetryOption`; `WithRetryInterval(time.Duration) RetryOption`.

- [ ] **Step 1: Write the failing tests**

Create `data/sqlite/tx_test.go`:
```go
package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func txDB(t *testing.T) (*sqlite.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tx.db")
	cfg := sqlite.DefaultConfig()
	cfg.Path = path
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlite.Close(db, nil) })
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t(id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	return db, path
}

func count(t *testing.T, db *sqlite.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestWithTx_CommitsOnSuccess(t *testing.T) {
	db, _ := txDB(t)
	err := sqlite.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO t(id) VALUES (1)`)
		return e
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if count(t, db) != 1 {
		t.Fatalf("row not committed")
	}
}

func TestWithTx_RollsBackOnError(t *testing.T) {
	db, _ := txDB(t)
	sentinel := errors.New("nope")
	err := sqlite.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO t(id) VALUES (1)`)
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if count(t, db) != 0 {
		t.Fatalf("row must have rolled back")
	}
}

func TestWithTx_RollsBackAndRepanics(t *testing.T) {
	db, _ := txDB(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("panic must propagate")
		}
		if count(t, db) != 0 {
			t.Fatalf("row must have rolled back on panic")
		}
	}()
	_ = sqlite.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO t(id) VALUES (1)`)
		panic("boom")
	})
}

func TestWithTxRetry_GivesUpOnHeldWriteLock(t *testing.T) {
	db, path := txDB(t)
	// Hold the write lock from an independent connection with busy_timeout=0.
	blocker, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate&_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("blocker open: %v", err)
	}
	defer blocker.Close()
	held, err := blocker.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("blocker begin: %v", err)
	}
	if _, err := held.Exec(`INSERT INTO t(id) VALUES (999)`); err != nil {
		t.Fatalf("blocker write: %v", err)
	}
	defer held.Rollback()

	// Force our writer to busy out fast: reopen with busy_timeout 0.
	cfg := sqlite.DefaultConfig()
	cfg.Path = path
	cfg.BusyTimeout = 0
	fast, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		t.Fatalf("fast open: %v", err)
	}
	defer sqlite.Close(fast, nil)

	err = sqlite.WithTxRetry(context.Background(), fast, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO t(id) VALUES (2)`)
		return e
	}, sqlite.WithRetryAttempts(3), sqlite.WithRetryInterval(time.Millisecond))
	if err == nil || !sqlite.IsBusy(err) {
		t.Fatalf("want busy error after retries, got %v", err)
	}
}

func TestWithTxRetry_HonorsContextCancel(t *testing.T) {
	db, _ := txDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sqlite.WithTxRetry(ctx, db, func(tx *sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("want error on cancelled ctx")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestWithTx' ./data/sqlite/`
Expected: build FAILS — `undefined: sqlite.WithTx` / `sqlite.WithTxRetry` / `sqlite.WithRetryAttempts`.

- [ ] **Step 3: Write tx.go**

Create `data/sqlite/tx.go`:
```go
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// maxRetryBackoff caps the wait between WithTxRetry attempts.
const maxRetryBackoff = 30 * time.Second

// retryConfig holds the resolved WithTxRetry knobs.
type retryConfig struct {
	interval time.Duration
	attempts int
}

func defaultRetryConfig() retryConfig {
	return retryConfig{attempts: 3, interval: 50 * time.Millisecond}
}

// RetryOption tunes WithTxRetry's busy-retry loop.
type RetryOption func(*retryConfig)

// WithRetryAttempts sets the total number of attempts (including the first). Values
// below 1 are clamped to 1. Default 3.
func WithRetryAttempts(n int) RetryOption {
	return func(c *retryConfig) {
		if n < 1 {
			n = 1
		}
		c.attempts = n
	}
}

// WithRetryInterval sets the base backoff between retries (interval · 2^attempt,
// capped at maxRetryBackoff). Values <= 0 are ignored. Default 50ms.
func WithRetryInterval(d time.Duration) RetryOption {
	return func(c *retryConfig) {
		if d > 0 {
			c.interval = d
		}
	}
}

// WithTx begins a transaction on the writer pool (BEGIN IMMEDIATE via the writer's
// _txlock, so the write lock is taken upfront), runs fn, and commits on success or
// rolls back on error. If fn panics, the transaction is rolled back and the panic is
// re-raised. The rollback's own error is ignored once fn has already failed.
func WithTx(ctx context.Context, db *DB, fn func(*sql.Tx) error) error {
	tx, err := db.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	panicked := true
	defer func() {
		if panicked {
			_ = tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		panicked = false
		_ = tx.Rollback()
		return err
	}

	panicked = false
	return tx.Commit()
}

// WithTxRetry is WithTx plus an automatic retry loop: when the transaction fails with
// a busy/locked condition (IsBusy) it backs off and retries up to the configured
// attempt budget. Any other error returns immediately. A panic propagates without
// retry (WithTx re-raises it).
func WithTxRetry(ctx context.Context, db *DB, fn func(*sql.Tx) error, opts ...RetryOption) error {
	rc := defaultRetryConfig()
	for _, opt := range opts {
		opt(&rc)
	}

	var lastErr error
	for attempt := range rc.attempts {
		err := WithTx(ctx, db, fn)
		if err == nil {
			return nil
		}
		if !IsBusy(err) {
			return err
		}
		lastErr = err

		if attempt == rc.attempts-1 {
			break
		}
		timer := time.NewTimer(backoff(rc.interval, attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(lastErr, ctx.Err())
		case <-timer.C:
		}
	}
	return lastErr
}

// backoff returns interval · 2^attempt, capped at maxRetryBackoff.
func backoff(interval time.Duration, attempt int) time.Duration {
	if interval <= 0 {
		return 0
	}
	wait := interval << attempt
	if wait <= 0 || wait > maxRetryBackoff {
		return maxRetryBackoff
	}
	return wait
}
```

- [ ] **Step 4: Format and run the tests to verify they pass**

Run:
```bash
just fmt ./data/sqlite/...
go test -race -run 'TestWithTx' ./data/sqlite/
```
Expected: PASS. (The `TestWithTxRetry_HonorsContextCancel` case relies on `WithTx` failing fast under a cancelled ctx; if a cancelled-ctx `BeginTx` returns a non-busy error the loop returns it immediately, which still satisfies the assertion.)

- [ ] **Step 5: Commit**

```bash
git add data/sqlite/tx.go data/sqlite/tx_test.go
git commit -m "feat(sqlite): WithTx and WithTxRetry over the writer pool"
```

---

### Task 7: `data/sqlite` — Migrator seam end-to-end

`migrator.go` already holds the `Migrator` interface (created in Task 4). This task adds the `WithMigrator` option, wires it (already referenced in `Open`), and proves the M1 integration with `migration.WithDialect(SQLite)`.

**Files:**
- Modify: `data/sqlite/options.go` (add `WithMigrator`)
- Modify: `data/sqlite/migrator.go` (finalize doc comment)
- Test: `data/sqlite/migrator_test.go`

**Interfaces:**
- Consumes: `Migrator`, `Open`, `WithConfig`, `migration.New`, `migration.WithDialect`, `migration.SQLite`.
- Produces: `WithMigrator(Migrator) Option`.

- [ ] **Step 1: Write the failing test**

Create `data/sqlite/migrator_test.go`:
```go
package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/sqlite"
)

func TestWithMigrator_AppliesSQLiteMigrationsOnOpen(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_init.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nCREATE TABLE widgets (id INTEGER PRIMARY KEY);\n"),
		},
	}
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "m.db")

	db, err := sqlite.Open(context.Background(),
		sqlite.WithConfig(cfg),
		sqlite.WithMigrator(migration.New(fsys, migration.WithDialect(migration.SQLite))),
	)
	if err != nil {
		t.Fatalf("Open with migrator: %v", err)
	}
	defer sqlite.Close(db, nil)

	// Migration ran on the writer; the reader sees the schema.
	var name string
	if err := db.Reader().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='widgets'`).Scan(&name); err != nil {
		t.Fatalf("widgets table not visible to reader: %v", err)
	}
}

func TestWithMigrator_NilRejected(t *testing.T) {
	cfg := sqlite.DefaultConfig()
	cfg.Path = "app.db"
	if _, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg), sqlite.WithMigrator(nil)); !errors.Is(err, sqlite.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestWithMigrator_FailureFailsOpen(t *testing.T) {
	fsys := fstest.MapFS{
		"00001_bad.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nCREATE TABLE ;\n"), // invalid SQL
		},
	}
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(t.TempDir(), "bad.db")
	if _, err := sqlite.Open(context.Background(),
		sqlite.WithConfig(cfg),
		sqlite.WithMigrator(migration.New(fsys, migration.WithDialect(migration.SQLite))),
	); err == nil {
		t.Fatal("bad migration must fail Open")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestWithMigrator ./data/sqlite/`
Expected: build FAILS — `undefined: sqlite.WithMigrator`.

- [ ] **Step 3: Add WithMigrator to options.go**

Append to `data/sqlite/options.go`:
```go
// WithMigrator registers a Migrator that Open runs against the writer pool after both
// pools are live and pinged, before Open returns. A failed migration fails Open. A nil
// Migrator is rejected (ErrInvalidConfig). Pass migration.New(fsys,
// migration.WithDialect(migration.SQLite)) — *migration.Migrator satisfies Migrator.
func WithMigrator(m Migrator) Option {
	return func(c *config) {
		if m == nil {
			c.errs = append(c.errs, fmt.Errorf("%w: WithMigrator received a nil Migrator", ErrInvalidConfig))
			return
		}
		c.migrator = m
	}
}
```

- [ ] **Step 4: Format and run the tests to verify they pass**

Run:
```bash
just fmt ./data/sqlite/...
go test -race -run TestWithMigrator ./data/sqlite/
```
Expected: PASS.

- [ ] **Step 5: Run the whole package once**

Run: `just test ./data/sqlite/...`
Expected: PASS with coverage reported; `-race` clean.

- [ ] **Step 6: Commit**

```bash
git add data/sqlite/options.go data/sqlite/migrator.go data/sqlite/migrator_test.go
git commit -m "feat(sqlite): WithMigrator seam with SQLite-dialect migration integration"
```

---

### Task 8: `data/sqlite` — benchmarks + optimization pass

**Files:**
- Create: `data/sqlite/bench_test.go`

**Interfaces:**
- Consumes: `Open`, `WithConfig`, `WithTx`, `DB` methods.

- [ ] **Step 1: Write the benchmarks**

Create `data/sqlite/bench_test.go`:
```go
package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dmitrymomot/forge/data/sqlite"
)

func benchDB(b *testing.B) *sqlite.DB {
	b.Helper()
	cfg := sqlite.DefaultConfig()
	cfg.Path = filepath.Join(b.TempDir(), "bench.db")
	db, err := sqlite.Open(context.Background(), sqlite.WithConfig(cfg))
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { sqlite.Close(db, nil) })
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t(id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		b.Fatalf("create: %v", err)
	}
	return db
}

func BenchmarkInsert(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO t(v) VALUES (?)`, "x"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSelect(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO t(id, v) VALUES (1, 'x')`); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v string
		if err := db.QueryRowContext(ctx, `SELECT v FROM t WHERE id=1`).Scan(&v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWithTx(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := sqlite.WithTx(ctx, db, func(tx *sql.Tx) error {
			_, e := tx.Exec(`INSERT INTO t(v) VALUES (?)`, "x")
			return e
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConcurrentReadWrite(b *testing.B) {
	db := benchDB(b)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO t(id, v) VALUES (1, 'x')`); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var v string
			if err := db.QueryRowContext(ctx, `SELECT v FROM t WHERE id=1`).Scan(&v); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
```

- [ ] **Step 2: Run the benchmarks and capture baseline numbers**

Run: `just bench ./data/sqlite/...`
Expected: all benchmarks run and report `ns/op` + `allocs/op`. Save this output as the "before" baseline for the PR.

- [ ] **Step 3: Optimization pass**

Review the wrapper hot paths (`ExecContext`/`QueryContext`/`QueryRowContext`, `buildDSN`) against the baseline. Apply an optimization ONLY if a benchmark proves a win (e.g. preallocating the `params` slice in `buildDSN` — already sized; verify no reallocation). If no measured win exists, record "no changes — wrapper adds negligible overhead over database/sql" in the PR. Do NOT add speculative complexity. If any change is made, re-run `just bench ./data/sqlite/...` and record before/after.

- [ ] **Step 4: Commit**

```bash
git add data/sqlite/bench_test.go
git commit -m "test(sqlite): benchmarks and optimization pass"
```

---

### Task 9: `data/sqlite` — doc.go, roadmap removal, final lint

**Files:**
- Create: `data/sqlite/doc.go`
- Modify: `docs/packages.md` (delete the `data/sqlite` entry)

**Interfaces:** none (documentation + cleanup).

- [ ] **Step 1: Write doc.go with a runnable example**

Create `data/sqlite/doc.go`:
```go
// Package sqlite turns a Config into a live, throughput-tuned, cgo-free SQLite
// database built on modernc.org/sqlite, with a reader/writer connection-pool split,
// WAL and pragma discipline, and clean shutdown.
//
// Open builds two database/sql pools over one file: a single pinned writer connection
// (BEGIN IMMEDIATE, WAL) so writes serialize inside Go and never race into
// SQLITE_BUSY, and an N-connection reader pool (query_only) for concurrent WAL reads.
// Send writes and read-your-writes queries to Writer(), read-only queries to Reader();
// the convenience ExecContext/QueryContext/QueryRowContext/BeginTx methods route by
// that same convention. Hand Healthcheck(db) to a readiness probe and defer
// Close(db, logger) in main.
//
//	func main() {
//		ctx, stop := supervisor.NewContext()
//		defer stop()
//
//		cfg := sqlite.DefaultConfig()
//		cfg.Path = "app.db"
//		_ = env.Parse(&cfg)
//
//		db, err := sqlite.Open(ctx,
//			sqlite.WithConfig(cfg),
//			sqlite.WithMigrator(migration.New(migrationsFS, migration.WithDialect(migration.SQLite))),
//		)
//		if err != nil {
//			slog.Error("db open failed", "err", err)
//			os.Exit(1)
//		}
//		defer sqlite.Close(db, slog.Default())
//
//		if err := supervisor.Run(ctx,
//			supervisor.WithService(httpserver.New(routes(db))),
//		); err != nil {
//			slog.Error("shutdown", "err", err)
//			os.Exit(1)
//		}
//	}
//
// Transactions: WithTx runs a function inside a writer transaction (commit on success,
// rollback on error, rollback-and-repanic on panic); WithTxRetry adds an automatic
// retry loop for busy/locked conditions (SQLITE_BUSY / SQLITE_LOCKED).
//
// Error classification: IsUniqueViolation, IsForeignKeyViolation, IsBusy, and
// IsNotFound match the underlying *sqlite.Error result code (or sql.ErrNoRows) so call
// sites branch without importing the driver.
//
// Sentinel errors — ErrInvalidConfig, ErrConnect, ErrHealthcheck — are single-line and
// matchable with errors.Is. Configuration is supplied through Config, whose env struct
// tags are inert; DefaultConfig is the single source of truth for defaults:
//
//	Field            Env var                    Default
//	Path             SQLITE_PATH                "" (required)
//	JournalMode      SQLITE_JOURNAL_MODE        WAL
//	Synchronous      SQLITE_SYNCHRONOUS         NORMAL
//	BusyTimeout      SQLITE_BUSY_TIMEOUT        5s
//	ForeignKeys      SQLITE_FOREIGN_KEYS        true
//	CacheSize        SQLITE_CACHE_SIZE          -16000 (~16 MiB)
//	MmapSize         SQLITE_MMAP_SIZE           268435456 (256 MiB)
//	ReadPoolSize     SQLITE_READ_POOL_SIZE      0 (=> runtime.NumCPU())
//	ConnMaxIdleTime  SQLITE_CONN_MAX_IDLE_TIME  0 (keep warm)
//	ConnMaxLifetime  SQLITE_CONN_MAX_LIFETIME   0 (unbounded)
//
// A Path of ":memory:" is rewritten to a unique shared-cache in-memory database so all
// reader connections see the same data and each Open stays isolated; WAL is skipped
// there (memory databases do not support it).
package sqlite
```

- [ ] **Step 2: Verify the example and docs render**

Run: `go doc ./data/sqlite`
Expected: package synopsis and the config table render; no error.

- [ ] **Step 3: Remove the roadmap entry**

In `docs/packages.md`, delete the entire `**data/sqlite**` entry (the heading, its paragraph, and its `Deps:` line, plus the surrounding `---` separator for that block) so the roadmap lists only unbuilt packages.

- [ ] **Step 4: Run the full lint and test gate**

Run:
```bash
just fmt ./data/sqlite/...
just fmt ./data/migration/...
just lint
just test ./data/sqlite/...
just test ./data/migration/...
```
Expected: `just lint` clean (go vet, build, golangci-lint, nilaway, betteralign, modernize all pass); tests PASS with `-race`.

Note if `nilaway` flags `db.writer`/`db.reader` as possibly nil in the routing methods or lifecycle: they are always set by a successful `Open`, and `Close`/`Healthcheck` guard a nil `db`. Only if nilaway cannot prove it, add a narrowly-scoped `//nolint:nilaway` with a one-line justification on the specific method — do not broaden.

- [ ] **Step 5: Commit**

```bash
git add data/sqlite/doc.go docs/packages.md
git commit -m "docs(sqlite): package doc, runnable example, remove roadmap entry"
```

---

## Self-Review Notes (for the implementer)

- **Spec coverage:** reader/writer split (Task 4) · per-connection pragma DSN incl. reader `query_only`-last / writer-only `journal_mode` (Task 3) · Config + `SQLITE_` env + no connect-retry (Task 2) · in-memory unique shared-cache rewrite (Tasks 3–4) · tx helpers (Task 6) · classification (Task 5) · `WithMigrator` + `migration.WithDialect` M1 (Tasks 1, 7) · live-everywhere tests + benchmarks (all + Task 8) · anti-scope respected (no query builder, no cgo, no `WithScope`, no connect-retry). All spec sections map to a task.
- **Driver facts verified:** modernc driver name `"sqlite"`; `*sqlite.Error.Code() int`; `_pragma=`/`_txlock=` DSN params; result codes BUSY 5 / LOCKED 6 (extended variants share the low byte) / CONSTRAINT_UNIQUE 2067 / PRIMARYKEY 1555 / FOREIGNKEY 787; `goose.DialectSQLite3` exists in goose v3.27.1.
- **Type consistency:** `buildDSN(cfg Config, extra []pragma, memory bool, memName string, write bool)` is defined in Task 3 and called with those exact args in Task 4's `Open`. `Migrator` (Task 4 `migrator.go`) is consumed by `WithMigrator` (Task 7) and `Open`. `IsBusy` (Task 5) is consumed by `WithTxRetry` (Task 6) — Task 5 precedes Task 6. `config`/`Option` defined in Task 4 `options.go`; `WithMigrator` appended in Task 7.
- **Ordering constraint:** Task 5 (classify) must land before Task 6 (tx) because `WithTxRetry` calls `IsBusy`. The `migrator.go` interface is created in Task 4 (Step 5) so `Open`/`options` compile before Task 7 finalizes the seam.
