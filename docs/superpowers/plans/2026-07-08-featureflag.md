# `ops/featureflag` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the standalone `ops/featureflag` package — serializable flag records, `enabled → deny → allow → rollout` evaluation, token targeting, layered providers, scope-aware cache — per the approved spec `docs/superpowers/specs/2026-07-08-featureflag-design.md`, delivered fully autonomously through a green, review-resolved PR.

**Architecture:** A `Client` evaluates flag data fetched through a one-method `Provider` store seam (static set from options, mutable `Memory`, consumer-side Postgres). All evaluation intelligence (kill switch, token-set deny/allow, FNV percentage bucketing, typeconv coercion) lives in the client so every backend behaves identically. A `Cached` decorator adds scope-aware TTL caching with singleflight and serve-stale-on-error.

**Tech Stack:** Go 1.26, stdlib + `core/typeconv`, `core/ctxkey`, `core/clock`, `resilience/singleflight`. testify for tests. No yaml.v3 import in production code (legacy func-style `UnmarshalYAML`); yaml.v3 allowed in tests only.

## Global Constraints

- Work ONLY on the current branch `claude/nervous-chatterjee-91d06f`; never switch branches.
- Black-box tests only: package `featureflag_test`.
- Options idiom (`type Option func(*config)`), NEVER builders.
- Single-line error sentinels in `errors.go`, `errors.Is`-matchable, message prefix `featureflag: `.
- slog attributes only in log records; single-line, no embedded blobs.
- Production code imports NOTHING beyond: stdlib, `github.com/dmitrymomot/forge/core/typeconv`, `core/ctxkey`, `core/clock`, `resilience/singleflight`. In particular NO `gopkg.in/yaml.v3` and NO `ops/config` imports in non-test files.
- After every file change: `just fmt ./ops/featureflag/...` (package-path form — single-file form trips a spurious betteralign "undefined" error).
- Before finishing any task: `just test ./ops/featureflag/...` green.
- Go 1.26: use `new(expr)` where a pointer literal is needed; run `just lint` (includes modernize, betteralign, nilaway) at the marked checkpoints.
- NEVER add Claude attribution lines to commits, PR title/body, or PR comments (`Generated with Claude Code`, `Co-Authored-By: Claude`, etc.).
- Fully autonomous: no user questions; every failure discovered (tests, CI, review) is fixed in-session.

## File Structure

```
ops/featureflag/
├── doc.go            # package doc: usage ladder, contracts, pg + tenant self-service + maintenance-gate recipes
├── errors.go         # sentinels
├── flag.go           # Flag, Flags, YAML unmarshaling (func-style), clone
├── provider.go       # Provider, Lister interfaces
├── memory.go         # Memory mutable provider
├── options.go        # Option funcs + config
├── client.go         # Client, New, typed getters, provider chain, All
├── eval.go           # evaluation pipeline: matches, bucket (inline FNV-1a)
├── subject.go        # WithSubject carrier, Evaluator, For
├── cached.go         # Cached decorator (+ CacheOption)
├── flag_test.go
├── memory_test.go
├── client_test.go    # options, getters, pipeline, All
├── subject_test.go   # carrier + Evaluator
├── cached_test.go
└── bench_test.go
docs/packages.md      # move featureflag to shipped; drop planned bullet + recipe-owed line
```

---

### Task 1: Flag data model + YAML unmarshaling

**Files:**
- Create: `ops/featureflag/errors.go`
- Create: `ops/featureflag/flag.go`
- Test: `ops/featureflag/flag_test.go`

**Interfaces:**
- Consumes: `core/typeconv.Format(v any) string`.
- Produces: `type Flag struct{ Value string; Allow, Deny []string; Rollout int; Enabled bool }`; `type Flags map[string]Flag`; unexported `Flags.clone() Flags`; sentinels `ErrEmptyKey`, `ErrInvalidRollout`, `ErrUnknownFlag`, `ErrInvalidFlag`, `ErrNilProvider`. Both `Flag` and `Flags` implement `UnmarshalYAML(unmarshal func(any) error) error`.

- [ ] **Step 1: Write the failing tests**

```go
// ops/featureflag/flag_test.go
package featureflag_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3" // test-only; production code must not import it

	"github.com/dmitrymomot/forge/ops/featureflag"
)

func unmarshalFlags(t *testing.T, src string) featureflag.Flags {
	t.Helper()
	var doc struct {
		Flags featureflag.Flags `yaml:"flags"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	return doc.Flags
}

func TestFlagsUnmarshalYAML(t *testing.T) {
	t.Parallel()

	t.Run("scalar shorthand", func(t *testing.T) {
		t.Parallel()
		fs := unmarshalFlags(t, `
flags:
  dark_mode: true
  max_items: 25
  banner: "Summer sale"
  ratio: 1.5
`)
		assert.Equal(t, featureflag.Flag{Value: "true", Enabled: true, Rollout: 100}, fs["dark_mode"])
		assert.Equal(t, featureflag.Flag{Value: "25", Enabled: true, Rollout: 100}, fs["max_items"])
		assert.Equal(t, featureflag.Flag{Value: "Summer sale", Enabled: true, Rollout: 100}, fs["banner"])
		assert.Equal(t, featureflag.Flag{Value: "1.5", Enabled: true, Rollout: 100}, fs["ratio"])
	})

	t.Run("object form with defaults", func(t *testing.T) {
		t.Parallel()
		fs := unmarshalFlags(t, `
flags:
  new_checkout:
    value: true
    rollout: 25
    allow: [role:staff, cus_9f2k]
    deny: [segment:self_excluded]
  plain:
    value: "x"
`)
		assert.Equal(t, featureflag.Flag{
			Value:   "true",
			Enabled: true,
			Rollout: 25,
			Allow:   []string{"role:staff", "cus_9f2k"},
			Deny:    []string{"segment:self_excluded"},
		}, fs["new_checkout"])
		// omitted enabled → true, omitted rollout → 100
		assert.Equal(t, featureflag.Flag{Value: "x", Enabled: true, Rollout: 100}, fs["plain"])
	})

	t.Run("explicit disable", func(t *testing.T) {
		t.Parallel()
		fs := unmarshalFlags(t, "flags:\n  off_flag: {value: true, enabled: false}\n")
		assert.False(t, fs["off_flag"].Enabled)
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		cases := map[string]struct {
			src  string
			want error
		}{
			"rollout too high":  {"flags:\n  f: {value: true, rollout: 101}\n", featureflag.ErrInvalidRollout},
			"rollout negative":  {"flags:\n  f: {value: true, rollout: -1}\n", featureflag.ErrInvalidRollout},
			"empty key":         {"flags:\n  \"\": true\n", featureflag.ErrEmptyKey},
			"null value":        {"flags:\n  f:\n", featureflag.ErrInvalidFlag},
			"unknown field":     {"flags:\n  f: {value: true, rollut: 5}\n", featureflag.ErrInvalidFlag},
			"sequence value":    {"flags:\n  f: [a, b]\n", featureflag.ErrInvalidFlag},
			"non-string tokens": {"flags:\n  f: {value: true, allow: [1, 2]}\n", featureflag.ErrInvalidFlag},
			"empty token":       {"flags:\n  f: {value: true, deny: [\"\"]}\n", featureflag.ErrInvalidFlag},
			"bad enabled type":  {"flags:\n  f: {value: true, enabled: yes please}\n", featureflag.ErrInvalidFlag},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var doc struct {
					Flags featureflag.Flags `yaml:"flags"`
				}
				err := yaml.Unmarshal([]byte(tc.src), &doc)
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.want)
			})
		}
	})
}

func TestFlagZeroValueDisabled(t *testing.T) {
	t.Parallel()
	assert.False(t, featureflag.Flag{}.Enabled, "zero value must be fail-safe disabled")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ops/featureflag/ -run TestFlag -v`
Expected: FAIL to build — package does not exist yet.

- [ ] **Step 3: Write errors.go and flag.go**

```go
// ops/featureflag/errors.go
package featureflag

import "errors"

var (
	// ErrEmptyKey reports an empty flag key in a source or option.
	ErrEmptyKey = errors.New("featureflag: empty flag key")
	// ErrInvalidRollout reports a rollout percent outside 0-100.
	ErrInvalidRollout = errors.New("featureflag: rollout must be between 0 and 100")
	// ErrUnknownFlag reports an adjuster option targeting a key absent from the static set.
	ErrUnknownFlag = errors.New("featureflag: unknown flag")
	// ErrInvalidFlag reports a malformed flag definition.
	ErrInvalidFlag = errors.New("featureflag: invalid flag definition")
	// ErrNilProvider reports a nil Provider passed to WithProvider or Cached.
	ErrNilProvider = errors.New("featureflag: nil provider")
)
```

```go
// ops/featureflag/flag.go
package featureflag

import (
	"fmt"
	"slices"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// Flag is a serializable flag definition. The zero value is disabled
// (fail-safe): getters return the caller's default until Enabled is set.
type Flag struct {
	Value   string   // canonical string form; typed getters coerce via typeconv
	Allow   []string // tokens: any match with the subject's token set → always on
	Deny    []string // tokens: any match → always off (wins over Allow)
	Rollout int      // 0-100 percent of subjects; 100 = everyone
	Enabled bool     // kill switch; false → getters return the caller default
}

// Flags is a flag set keyed by flag name. It embeds directly in an
// application config struct loaded by ops/config.
type Flags map[string]Flag

// UnmarshalYAML accepts either scalar shorthand (dark_mode: true) or object
// form ({value, enabled, rollout, allow, deny}). Implemented via the legacy
// func-style unmarshaler so this package does not import yaml.v3.
func (f *Flag) UnmarshalYAML(unmarshal func(any) error) error {
	var m map[string]any
	if err := unmarshal(&m); err == nil {
		return f.fromMap(m)
	}
	var scalar any
	if err := unmarshal(&scalar); err != nil {
		return err
	}
	return f.fromScalar(scalar)
}

func (f *Flag) fromMap(m map[string]any) error {
	out := Flag{Enabled: true, Rollout: 100}
	for k, v := range m {
		switch k {
		case "value":
			if !isScalar(v) {
				return fmt.Errorf("%w: value must be a scalar", ErrInvalidFlag)
			}
			out.Value = typeconv.Format(v)
		case "enabled":
			b, ok := v.(bool)
			if !ok {
				return fmt.Errorf("%w: enabled must be a bool", ErrInvalidFlag)
			}
			out.Enabled = b
		case "rollout":
			n, ok := v.(int)
			if !ok {
				return fmt.Errorf("%w: rollout must be an integer", ErrInvalidFlag)
			}
			if n < 0 || n > 100 {
				return fmt.Errorf("%w: got %d", ErrInvalidRollout, n)
			}
			out.Rollout = n
		case "allow":
			ts, err := tokenList(v)
			if err != nil {
				return fmt.Errorf("allow: %w", err)
			}
			out.Allow = ts
		case "deny":
			ts, err := tokenList(v)
			if err != nil {
				return fmt.Errorf("deny: %w", err)
			}
			out.Deny = ts
		default:
			return fmt.Errorf("%w: unknown field %q", ErrInvalidFlag, k)
		}
	}
	*f = out
	return nil
}

func (f *Flag) fromScalar(v any) error {
	if !isScalar(v) {
		return fmt.Errorf("%w: flag must be a scalar or a mapping", ErrInvalidFlag)
	}
	*f = Flag{Value: typeconv.Format(v), Enabled: true, Rollout: 100}
	return nil
}

func isScalar(v any) bool {
	switch v.(type) {
	case bool, int, int64, uint64, float64, string:
		return true
	default:
		return false
	}
}

func tokenList(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: token list must be a sequence of strings", ErrInvalidFlag)
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("%w: token must be a string", ErrInvalidFlag)
		}
		if s == "" {
			return nil, fmt.Errorf("%w: empty token", ErrInvalidFlag)
		}
		out = append(out, s)
	}
	return out, nil
}

// UnmarshalYAML validates keys while delegating per-flag parsing to Flag.
func (fs *Flags) UnmarshalYAML(unmarshal func(any) error) error {
	var m map[string]Flag
	if err := unmarshal(&m); err != nil {
		return err
	}
	for k := range m {
		if k == "" {
			return ErrEmptyKey
		}
	}
	*fs = m
	return nil
}

// clone deep-copies the set so a Client stays immutable after New.
func (fs Flags) clone() Flags {
	out := make(Flags, len(fs))
	for k, f := range fs {
		f.Allow = slices.Clone(f.Allow)
		f.Deny = slices.Clone(f.Deny)
		out[k] = f
	}
	return out
}
```

Note: `map[string]Flag` element decoding invokes `Flag.UnmarshalYAML` — yaml.v3 supports the legacy `obsoleteUnmarshaler` signature (verified in yaml.go:41 of v3.0.1).

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./ops/featureflag/... && go test ./ops/featureflag/ -run 'TestFlag' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/errors.go ops/featureflag/flag.go ops/featureflag/flag_test.go
git commit -m "feat(featureflag): Flag/Flags data model with YAML unmarshaling"
```

---

### Task 2: Provider seam + Memory provider

**Files:**
- Create: `ops/featureflag/provider.go`
- Create: `ops/featureflag/memory.go`
- Test: `ops/featureflag/memory_test.go`

**Interfaces:**
- Consumes: `Flag`, `Flags`, `Flags.clone()`, sentinels from Task 1.
- Produces: `type Provider interface{ Flag(ctx context.Context, key string) (Flag, bool, error) }`; `type Lister interface{ All(ctx context.Context) (Flags, error) }`; `func NewMemory(initial Flags) *Memory`; `func (m *Memory) Flag(ctx, key) (Flag, bool, error)`; `func (m *Memory) Set(key string, f Flag) error`; `func (m *Memory) Delete(key string)`; `func (m *Memory) All(ctx) (Flags, error)`.

- [ ] **Step 1: Write the failing tests**

```go
// ops/featureflag/memory_test.go
package featureflag_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/featureflag"
)

func TestMemoryProvider(t *testing.T) {
	t.Parallel()

	t.Run("initial set and lookup", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(featureflag.Flags{
			"dark_mode": {Value: "true", Enabled: true, Rollout: 100},
		})
		f, ok, err := m.Flag(t.Context(), "dark_mode")
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "true", f.Value)

		_, ok, err = m.Flag(t.Context(), "missing")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("nil initial is empty", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		_, ok, err := m.Flag(t.Context(), "any")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("Set validates and is visible", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		require.ErrorIs(t, m.Set("", featureflag.Flag{Enabled: true}), featureflag.ErrEmptyKey)
		require.ErrorIs(t, m.Set("f", featureflag.Flag{Enabled: true, Rollout: 101}), featureflag.ErrInvalidRollout)

		require.NoError(t, m.Set("maintenance", featureflag.Flag{Value: "true", Enabled: true, Rollout: 100}))
		f, ok, err := m.Flag(t.Context(), "maintenance")
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "true", f.Value)
	})

	t.Run("Set clones token slices", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		allow := []string{"role:staff"}
		require.NoError(t, m.Set("f", featureflag.Flag{Value: "x", Enabled: true, Rollout: 100, Allow: allow}))
		allow[0] = "role:hacked"
		f, _, _ := m.Flag(t.Context(), "f")
		assert.Equal(t, []string{"role:staff"}, f.Allow)
	})

	t.Run("Delete", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(featureflag.Flags{"f": {Value: "x", Enabled: true, Rollout: 100}})
		m.Delete("f")
		_, ok, _ := m.Flag(t.Context(), "f")
		assert.False(t, ok)
	})

	t.Run("All returns a copy", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(featureflag.Flags{"f": {Value: "x", Enabled: true, Rollout: 100}})
		all, err := m.All(t.Context())
		require.NoError(t, err)
		all["f"] = featureflag.Flag{Value: "mutated"}
		f, _, _ := m.Flag(t.Context(), "f")
		assert.Equal(t, "x", f.Value)
	})

	t.Run("concurrent Set and Flag", func(t *testing.T) {
		t.Parallel()
		m := featureflag.NewMemory(nil)
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_ = m.Set("f", featureflag.Flag{Value: "true", Enabled: i%2 == 0, Rollout: 100})
			}()
			go func() {
				defer wg.Done()
				_, _, _ = m.Flag(t.Context(), "f")
			}()
		}
		wg.Wait()
	})
}

// compile-time seam checks
var (
	_ featureflag.Provider = (*featureflag.Memory)(nil)
	_ featureflag.Lister   = (*featureflag.Memory)(nil)
)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ops/featureflag/ -run TestMemoryProvider -v`
Expected: FAIL to build — `Provider`, `Memory` undefined.

- [ ] **Step 3: Write provider.go and memory.go**

```go
// ops/featureflag/provider.go
package featureflag

import "context"

// Provider supplies flag definitions. Implementations must be safe for
// concurrent use. A miss is (Flag{}, false, nil). The client treats an error
// as a miss for that provider (logs it and consults the next one), so
// evaluation stays fail-safe.
//
// The ctx parameter is how multi-tenancy works: a database-backed provider
// reads the tenant ID from request context and keys its lookup on
// (tenant, key). The core package never learns about tenants.
type Provider interface {
	Flag(ctx context.Context, key string) (Flag, bool, error)
}

// Lister is an optional Provider upgrade for debug/admin visibility.
// Client.All merges results across providers that implement it.
type Lister interface {
	All(ctx context.Context) (Flags, error)
}
```

```go
// ops/featureflag/memory.go
package featureflag

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Memory is a mutable in-process Provider for runtime toggles (a guarded
// admin handler flipping a maintenance flag) and tests.
type Memory struct {
	mu    sync.RWMutex
	flags Flags
}

// NewMemory returns a Memory provider seeded with a deep copy of initial.
func NewMemory(initial Flags) *Memory {
	return &Memory{flags: initial.clone()}
}

// Flag implements Provider.
func (m *Memory) Flag(_ context.Context, key string) (Flag, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.flags[key]
	return f, ok, nil
}

// Set stores a validated flag; token slices are cloned.
func (m *Memory) Set(key string, f Flag) error {
	if key == "" {
		return ErrEmptyKey
	}
	if f.Rollout < 0 || f.Rollout > 100 {
		return fmt.Errorf("%w: got %d", ErrInvalidRollout, f.Rollout)
	}
	f.Allow = slices.Clone(f.Allow)
	f.Deny = slices.Clone(f.Deny)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flags[key] = f
	return nil
}

// Delete removes a flag; deleting a missing key is a no-op.
func (m *Memory) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.flags, key)
}

// All implements Lister; the result is a deep copy.
func (m *Memory) All(_ context.Context) (Flags, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.flags.clone(), nil
}
```

Note: `Flags.clone()` on a nil map returns an empty non-nil map, so `NewMemory(nil)` is safe.

- [ ] **Step 4: Run tests (with race) to verify they pass**

Run: `just fmt ./ops/featureflag/... && go test -race ./ops/featureflag/ -run TestMemoryProvider -v`
Expected: PASS, no race reports.

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/provider.go ops/featureflag/memory.go ops/featureflag/memory_test.go
git commit -m "feat(featureflag): Provider/Lister seam and mutable Memory provider"
```

---

### Task 3: Options + New + validation

**Files:**
- Create: `ops/featureflag/options.go`
- Create: `ops/featureflag/client.go` (construction half — getters arrive in Task 4)
- Test: `ops/featureflag/client_test.go` (construction section)

**Interfaces:**
- Consumes: `Flag`, `Flags`, `Provider`, sentinels.
- Produces: `type Option func(*config)`; `func New(opts ...Option) (*Client, error)`; source options `WithProvider(Provider)`, `WithFlags(Flags)`, `WithBool(string, bool)`, `WithString(string, string)`, `WithInt(string, int)`, `WithFloat64(string, float64)`, `WithDuration(string, time.Duration)`; adjusters `WithRollout(string, int)`, `WithAllow(string, ...string)`, `WithDeny(string, ...string)`; `WithIdentity(func(context.Context) []string)`, `WithLogger(*slog.Logger)`. Internal: `Client{providers []Provider; identity func(context.Context) []string; logger *slog.Logger}`; unexported `staticProvider` (implements Provider + Lister).

Semantics locked by the spec: sources apply in option order; the static set occupies the position of the FIRST static option in the provider chain; adjusters apply after all sources and require the key to exist in the static set (`ErrUnknownFlag`).

- [ ] **Step 1: Write the failing tests**

```go
// ops/featureflag/client_test.go
package featureflag_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/featureflag"
)

// fakeProvider returns canned flags and optionally errors.
type fakeProvider struct {
	flags featureflag.Flags
	err   error
	calls int
}

func (p *fakeProvider) Flag(_ context.Context, key string) (featureflag.Flag, bool, error) {
	p.calls++
	if p.err != nil {
		return featureflag.Flag{}, false, p.err
	}
	f, ok := p.flags[key]
	return f, ok, nil
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	t.Run("nil provider", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithProvider(nil))
		assert.ErrorIs(t, err, featureflag.ErrNilProvider)
	})

	t.Run("empty key in typed option", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithBool("", true))
		assert.ErrorIs(t, err, featureflag.ErrEmptyKey)
	})

	t.Run("empty key in WithFlags", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithFlags(featureflag.Flags{"": {Enabled: true}}))
		assert.ErrorIs(t, err, featureflag.ErrEmptyKey)
	})

	t.Run("invalid rollout in WithFlags entry", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithFlags(featureflag.Flags{"f": {Enabled: true, Rollout: 200}}))
		assert.ErrorIs(t, err, featureflag.ErrInvalidRollout)
	})

	t.Run("adjuster on unknown key", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(featureflag.WithRollout("nope", 50))
		assert.ErrorIs(t, err, featureflag.ErrUnknownFlag)
	})

	t.Run("adjuster invalid rollout", func(t *testing.T) {
		t.Parallel()
		_, err := featureflag.New(
			featureflag.WithBool("f", true),
			featureflag.WithRollout("f", 101),
		)
		assert.ErrorIs(t, err, featureflag.ErrInvalidRollout)
	})

	t.Run("no options is a valid empty client", func(t *testing.T) {
		t.Parallel()
		c, err := featureflag.New()
		require.NoError(t, err)
		assert.False(t, c.Bool(t.Context(), "anything", false))
	})
}

func TestProviderPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("first hit wins across providers", func(t *testing.T) {
		t.Parallel()
		first := &fakeProvider{flags: featureflag.Flags{"f": {Value: "first", Enabled: true, Rollout: 100}}}
		second := &fakeProvider{flags: featureflag.Flags{"f": {Value: "second", Enabled: true, Rollout: 100}}}
		c, err := featureflag.New(featureflag.WithProvider(first), featureflag.WithProvider(second))
		require.NoError(t, err)
		assert.Equal(t, "first", c.String(t.Context(), "f", ""))
	})

	t.Run("static set sits at position of first static option", func(t *testing.T) {
		t.Parallel()
		pg := &fakeProvider{flags: featureflag.Flags{"f": {Value: "db", Enabled: true, Rollout: 100}}}
		// provider first, static second → provider wins
		c, err := featureflag.New(
			featureflag.WithProvider(pg),
			featureflag.WithString("f", "static"),
		)
		require.NoError(t, err)
		assert.Equal(t, "db", c.String(t.Context(), "f", ""))

		// static first, provider second → static wins
		c2, err := featureflag.New(
			featureflag.WithString("f", "static"),
			featureflag.WithProvider(pg),
		)
		require.NoError(t, err)
		assert.Equal(t, "static", c2.String(t.Context(), "f", ""))
	})

	t.Run("later static option overrides earlier same key", func(t *testing.T) {
		t.Parallel()
		c, err := featureflag.New(
			featureflag.WithFlags(featureflag.Flags{"f": {Value: "config", Enabled: true, Rollout: 100}}),
			featureflag.WithString("f", "code"),
		)
		require.NoError(t, err)
		assert.Equal(t, "code", c.String(t.Context(), "f", ""))
	})

	t.Run("provider error falls through to next provider", func(t *testing.T) {
		t.Parallel()
		broken := &fakeProvider{err: assert.AnError}
		c, err := featureflag.New(
			featureflag.WithProvider(broken),
			featureflag.WithBool("f", true),
		)
		require.NoError(t, err)
		assert.True(t, c.Bool(t.Context(), "f", false))
	})
}

func TestTypedSourceOptions(t *testing.T) {
	t.Parallel()
	c, err := featureflag.New(
		featureflag.WithBool("b", true),
		featureflag.WithString("s", "hello"),
		featureflag.WithInt("i", 42),
		featureflag.WithFloat64("f", 1.5),
		featureflag.WithDuration("d", 5*time.Second),
		featureflag.WithRollout("b", 100),
		featureflag.WithAllow("b", "role:staff"),
		featureflag.WithDeny("b", "cus_bad"),
	)
	require.NoError(t, err)
	assert.True(t, c.Bool(t.Context(), "b", false))
	assert.Equal(t, "hello", c.String(t.Context(), "s", ""))
	assert.Equal(t, 42, c.Int(t.Context(), "i", 0))
	assert.InDelta(t, 1.5, c.Float64(t.Context(), "f", 0), 1e-9)
	assert.Equal(t, 5*time.Second, c.Duration(t.Context(), "d", 0))
}
```

Note: this file also exercises `String`/`Bool`/`Int`/`Float64`/`Duration` getters, which are implemented in Task 4. Tasks 3 and 4 are committed separately but Task 3's test run is scoped to `TestNewValidation` with getters temporarily exercised only through `Bool`/`String` — implement minimal getters in this task (lookup + Enabled + raw value for String, ParseBool for Bool, and the other three typed getters as thin parses). Full pipeline semantics (subject, tokens, rollout) land in Task 4.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ops/featureflag/ -run 'TestNewValidation|TestProviderPrecedence|TestTypedSourceOptions' -v`
Expected: FAIL to build — `New`, options undefined.

- [ ] **Step 3: Write options.go and client.go (construction + minimal getters)**

```go
// ops/featureflag/options.go
package featureflag

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// Option configures New.
type Option func(*config)

type config struct {
	chain     []Provider // nil placeholder at staticIdx until New builds the static provider
	static    Flags
	adjust    []func(Flags) error
	identity  func(context.Context) []string
	logger    *slog.Logger
	staticIdx int
}

// WithProvider appends a Provider to the lookup chain; providers are
// consulted in option order, first hit wins.
func WithProvider(p Provider) Option {
	return func(c *config) { c.chain = append(c.chain, p) }
}

// WithFlags merges a flag set (typically loaded from the app's config via
// ops/config) into the static set. The static set occupies the chain
// position of the first static option.
func WithFlags(f Flags) Option {
	return func(c *config) {
		c.reserveStatic()
		for k, v := range f {
			c.static[k] = v
		}
	}
}

// WithBool declares a static bool flag (enabled, rollout 100).
func WithBool(key string, v bool) Option { return withStatic(key, typeconv.Format(v)) }

// WithString declares a static string flag (enabled, rollout 100).
func WithString(key, v string) Option { return withStatic(key, v) }

// WithInt declares a static int flag (enabled, rollout 100).
func WithInt(key string, v int) Option { return withStatic(key, typeconv.Format(v)) }

// WithFloat64 declares a static float flag (enabled, rollout 100).
func WithFloat64(key string, v float64) Option { return withStatic(key, typeconv.Format(v)) }

// WithDuration declares a static duration flag (enabled, rollout 100).
func WithDuration(key string, v time.Duration) Option { return withStatic(key, v.String()) }

func withStatic(key, value string) Option {
	return func(c *config) {
		c.reserveStatic()
		c.static[key] = Flag{Value: value, Enabled: true, Rollout: 100}
	}
}

func (c *config) reserveStatic() {
	if c.staticIdx >= 0 {
		return
	}
	c.staticIdx = len(c.chain)
	c.chain = append(c.chain, nil)
}

// WithRollout sets the rollout percent of an existing static flag.
func WithRollout(key string, percent int) Option {
	return func(c *config) {
		c.adjust = append(c.adjust, func(fs Flags) error {
			if percent < 0 || percent > 100 {
				return fmt.Errorf("%w: %q got %d", ErrInvalidRollout, key, percent)
			}
			f, ok := fs[key]
			if !ok {
				return fmt.Errorf("%w: %q", ErrUnknownFlag, key)
			}
			f.Rollout = percent
			fs[key] = f
			return nil
		})
	}
}

// WithAllow appends always-on tokens to an existing static flag.
func WithAllow(key string, tokens ...string) Option {
	return adjustTokens(key, tokens, func(f *Flag, ts []string) { f.Allow = append(f.Allow, ts...) })
}

// WithDeny appends always-off tokens to an existing static flag.
func WithDeny(key string, tokens ...string) Option {
	return adjustTokens(key, tokens, func(f *Flag, ts []string) { f.Deny = append(f.Deny, ts...) })
}

func adjustTokens(key string, tokens []string, apply func(*Flag, []string)) Option {
	return func(c *config) {
		c.adjust = append(c.adjust, func(fs Flags) error {
			for _, tok := range tokens {
				if tok == "" {
					return fmt.Errorf("%w: empty token for %q", ErrInvalidFlag, key)
				}
			}
			f, ok := fs[key]
			if !ok {
				return fmt.Errorf("%w: %q", ErrUnknownFlag, key)
			}
			apply(&f, tokens)
			fs[key] = f
			return nil
		})
	}
}

// WithIdentity registers the resolver producing the subject's extra tokens
// (role:staff, segment:vip). It runs on every getter call and MUST be O(1)
// over pre-loaded request state — never a database call.
func WithIdentity(fn func(ctx context.Context) []string) Option {
	return func(c *config) { c.identity = fn }
}

// WithLogger sets the logger for provider errors and coercion warnings.
// Without it evaluation is silent.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}
```

```go
// ops/featureflag/client.go
package featureflag

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/core/typeconv"
)

// Client evaluates feature flags over an ordered provider chain. It is
// immutable after New and safe for unlimited concurrent use.
type Client struct {
	identity  func(context.Context) []string
	logger    *slog.Logger
	providers []Provider
}

// New builds a Client from source options (WithProvider/WithFlags/typed
// WithXxx, applied in order — the static set occupies the position of the
// first static option) and adjusters (WithRollout/WithAllow/WithDeny,
// applied after all sources).
func New(opts ...Option) (*Client, error) {
	cfg := config{staticIdx: -1, static: Flags{}}
	for _, opt := range opts {
		opt(&cfg)
	}
	for k, f := range cfg.static {
		if k == "" {
			return nil, ErrEmptyKey
		}
		if f.Rollout < 0 || f.Rollout > 100 {
			return nil, fmt.Errorf("%w: %q got %d", ErrInvalidRollout, k, f.Rollout)
		}
	}
	for _, adj := range cfg.adjust {
		if err := adj(cfg.static); err != nil {
			return nil, err
		}
	}
	if cfg.staticIdx >= 0 {
		cfg.chain[cfg.staticIdx] = staticProvider{flags: cfg.static.clone()}
	}
	for _, p := range cfg.chain {
		if p == nil {
			return nil, ErrNilProvider
		}
	}
	return &Client{providers: cfg.chain, identity: cfg.identity, logger: cfg.logger}, nil
}

// staticProvider serves the immutable option-built flag set.
type staticProvider struct {
	flags Flags
}

func (p staticProvider) Flag(_ context.Context, key string) (Flag, bool, error) {
	f, ok := p.flags[key]
	return f, ok, nil
}

func (p staticProvider) All(_ context.Context) (Flags, error) {
	return p.flags.clone(), nil
}

// lookup consults providers in order; errors are logged and treated as a
// miss for that provider so evaluation stays fail-safe.
func (c *Client) lookup(ctx context.Context, key string) (Flag, bool) {
	for _, p := range c.providers {
		f, ok, err := p.Flag(ctx, key)
		if err != nil {
			c.warn(ctx, "featureflag: provider error", slog.String("flag", key), slog.Any("error", err))
			continue
		}
		if ok {
			return f, true
		}
	}
	return Flag{}, false
}

func (c *Client) warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	if c.logger == nil {
		return
	}
	c.logger.LogAttrs(ctx, slog.LevelWarn, msg, attrs...)
}

// Bool returns the flag coerced to bool, or def on any miss.
func (c *Client) Bool(ctx context.Context, key string, def bool) bool {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseBool(s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "bool")
		return def
	}
	return v
}

// String returns the flag value, or def on any miss.
func (c *Client) String(ctx context.Context, key, def string) string {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	return s
}

// Int returns the flag coerced to int, or def on any miss.
func (c *Client) Int(ctx context.Context, key string, def int) int {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseInt[int](s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "int")
		return def
	}
	return v
}

// Float64 returns the flag coerced to float64, or def on any miss.
func (c *Client) Float64(ctx context.Context, key string, def float64) float64 {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseFloat[float64](s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "float64")
		return def
	}
	return v
}

// Duration returns the flag coerced to time.Duration, or def on any miss.
func (c *Client) Duration(ctx context.Context, key string, def time.Duration) time.Duration {
	s, ok := c.value(ctx, key)
	if !ok {
		return def
	}
	v, err := typeconv.ParseDuration(s)
	if err != nil {
		c.warnCoerce(ctx, key, s, "duration")
		return def
	}
	return v
}

func (c *Client) warnCoerce(ctx context.Context, key, val, typ string) {
	c.warn(ctx, "featureflag: coercion failed",
		slog.String("flag", key), slog.String("value", val), slog.String("type", typ))
}

// value resolves the final string value; Task 4 extends this with the
// subject/token pipeline. At this stage: lookup + kill switch only.
func (c *Client) value(ctx context.Context, key string) (string, bool) {
	f, ok := c.lookup(ctx, key)
	if !ok || !f.Enabled {
		return "", false
	}
	return f.Value, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./ops/featureflag/... && go test -race ./ops/featureflag/ -run 'TestNewValidation|TestProviderPrecedence|TestTypedSourceOptions' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/options.go ops/featureflag/client.go ops/featureflag/client_test.go
git commit -m "feat(featureflag): options, New validation, provider chain, typed getters"
```

---

### Task 4: Evaluation pipeline — subject, tokens, deny/allow, rollout

**Files:**
- Create: `ops/featureflag/eval.go`
- Create: `ops/featureflag/subject.go` (carrier half; `Evaluator` arrives Task 5)
- Modify: `ops/featureflag/client.go` (replace `value` with the full pipeline)
- Test: `ops/featureflag/subject_test.go`

**Interfaces:**
- Consumes: Task 3's `Client`, `lookup`.
- Produces: `func WithSubject(ctx context.Context, id string) context.Context`; internal `func (c *Client) valueFor(ctx context.Context, key, id string, extra []string) (string, bool)`; `func matches(list []string, id string, extra []string) bool`; `func bucket(key, id string) int` (inline FNV-1a 64, `% 100`). Pipeline order: enabled → deny → allow → rollout.

- [ ] **Step 1: Write the failing tests**

```go
// ops/featureflag/subject_test.go
package featureflag_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/featureflag"
)

func newClient(t *testing.T, opts ...featureflag.Option) *featureflag.Client {
	t.Helper()
	c, err := featureflag.New(opts...)
	require.NoError(t, err)
	return c
}

func TestEvaluationPipeline(t *testing.T) {
	t.Parallel()

	t.Run("disabled flag returns default", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: false, Rollout: 100},
		}))
		assert.False(t, c.Bool(t.Context(), "f", false))
		assert.True(t, c.Bool(t.Context(), "f", true), "default is returned, not false")
	})

	t.Run("deny wins over allow", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 100,
				Allow: []string{"usr_1"}, Deny: []string{"usr_1"}},
		}))
		ctx := featureflag.WithSubject(t.Context(), "usr_1")
		assert.False(t, c.Bool(ctx, "f", false))
	})

	t.Run("allow bypasses rollout zero", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 0, Allow: []string{"usr_1"}},
		}))
		assert.True(t, c.Bool(featureflag.WithSubject(t.Context(), "usr_1"), "f", false))
		assert.False(t, c.Bool(featureflag.WithSubject(t.Context(), "usr_2"), "f", false))
	})

	t.Run("identity tokens match allow and deny", func(t *testing.T) {
		t.Parallel()
		c := newClient(t,
			featureflag.WithFlags(featureflag.Flags{
				"vip_only":  {Value: "true", Enabled: true, Rollout: 0, Allow: []string{"segment:vip"}},
				"norm_only": {Value: "true", Enabled: true, Rollout: 100, Deny: []string{"segment:vip"}},
			}),
			featureflag.WithIdentity(func(ctx context.Context) []string {
				if id, _ := fromCtx(ctx); id == "usr_vip" {
					return []string{"segment:vip"}
				}
				return nil
			}),
		)
		vip := featureflag.WithSubject(t.Context(), "usr_vip")
		norm := featureflag.WithSubject(t.Context(), "usr_norm")
		assert.True(t, c.Bool(vip, "vip_only", false))
		assert.False(t, c.Bool(norm, "vip_only", false))
		assert.False(t, c.Bool(vip, "norm_only", false))
		assert.True(t, c.Bool(norm, "norm_only", false))
	})

	t.Run("rollout without subject returns default", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 50},
		}))
		assert.False(t, c.Bool(t.Context(), "f", false), "no subject → deterministic off path")
	})

	t.Run("rollout 100 needs no subject", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithBool("f", true))
		assert.True(t, c.Bool(t.Context(), "f", false))
	})

	t.Run("empty subject id never matches empty-string tokens", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: 0, Allow: []string{"usr_1"}},
		}))
		// no subject in ctx: must not accidentally match anything
		assert.False(t, c.Bool(t.Context(), "f", false))
	})
}

// fromCtx mirrors what an app-side identity resolver does: it reads the
// subject the middleware set. Exercises that WithSubject round-trips.
func fromCtx(ctx context.Context) (string, bool) {
	return featureflag.SubjectFromContext(ctx)
}

func TestRolloutProperties(t *testing.T) {
	t.Parallel()

	flagAt := func(t *testing.T, percent int) *featureflag.Client {
		t.Helper()
		return newClient(t, featureflag.WithFlags(featureflag.Flags{
			"f": {Value: "true", Enabled: true, Rollout: percent},
		}))
	}
	inCohort := func(c *featureflag.Client, id string) bool {
		return c.Bool(featureflag.WithSubject(context.Background(), id), "f", false)
	}

	t.Run("deterministic", func(t *testing.T) {
		t.Parallel()
		c := flagAt(t, 50)
		first := inCohort(c, "usr_42")
		for range 100 {
			assert.Equal(t, first, inCohort(c, "usr_42"))
		}
	})

	t.Run("monotonic ramp keeps earlier cohort", func(t *testing.T) {
		t.Parallel()
		c25, c50 := flagAt(t, 25), flagAt(t, 50)
		for i := range 2000 {
			id := fmt.Sprintf("usr_%d", i)
			if inCohort(c25, id) {
				assert.True(t, inCohort(c50, id), "raising percent must never drop user %s", id)
			}
		}
	})

	t.Run("distribution roughly matches percent", func(t *testing.T) {
		t.Parallel()
		c := flagAt(t, 25)
		hits := 0
		const n = 2000
		for i := range n {
			if inCohort(c, fmt.Sprintf("usr_%d", i)) {
				hits++
			}
		}
		assert.InDelta(t, n*25/100, hits, n*5/100, "25%% ±5pp over %d ids", n)
	})

	t.Run("buckets decorrelated across flags", func(t *testing.T) {
		t.Parallel()
		c := newClient(t, featureflag.WithFlags(featureflag.Flags{
			"a": {Value: "true", Enabled: true, Rollout: 50},
			"b": {Value: "true", Enabled: true, Rollout: 50},
		}))
		same := 0
		const n = 2000
		for i := range n {
			ctx := featureflag.WithSubject(context.Background(), fmt.Sprintf("usr_%d", i))
			if c.Bool(ctx, "a", false) == c.Bool(ctx, "b", false) {
				same++
			}
		}
		// perfectly correlated would be n; independent ≈ n/2
		assert.Less(t, same, n*3/5, "flag buckets must not be correlated")
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ops/featureflag/ -run 'TestEvaluationPipeline|TestRolloutProperties' -v`
Expected: FAIL to build — `WithSubject`, `SubjectFromContext` undefined.

- [ ] **Step 3: Write eval.go, subject.go; replace client.value**

```go
// ops/featureflag/eval.go
package featureflag

// matches reports whether any token in list equals the subject id (when
// non-empty) or any extra identity token. No allocations.
func matches(list []string, id string, extra []string) bool {
	for _, t := range list {
		if id != "" && t == id {
			return true
		}
		for _, e := range extra {
			if t == e {
				return true
			}
		}
	}
	return false
}

// bucket maps (flag key, subject id) to [0,100) via FNV-1a 64 with a NUL
// separator, inlined to stay allocation-free on the hot path. Deterministic
// across processes; including the key decorrelates buckets across flags.
func bucket(key, id string) int {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := range len(key) {
		h ^= uint64(key[i])
		h *= prime64
	}
	h *= prime64 // NUL separator: h ^= 0 is a no-op, the multiply still mixes
	for i := range len(id) {
		h ^= uint64(id[i])
		h *= prime64
	}
	return int(h % 100)
}

// eval runs the data pipeline for an enabled flag:
// deny → allow → rollout. Returns the value or a miss.
func eval(f Flag, key, id string, extra []string) (string, bool) {
	if matches(f.Deny, id, extra) {
		return "", false
	}
	if matches(f.Allow, id, extra) {
		return f.Value, true
	}
	if f.Rollout >= 100 {
		return f.Value, true
	}
	if f.Rollout <= 0 || id == "" {
		return "", false
	}
	if bucket(key, id) < f.Rollout {
		return f.Value, true
	}
	return "", false
}
```

```go
// ops/featureflag/subject.go
package featureflag

import (
	"context"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

var subjectKey = ctxkey.New[string]("featureflag.subject")

// WithSubject attaches the evaluation subject ID (user/tenant/customer ID)
// to the context. Auth middleware calls this once per request. IDs should be
// globally unique (id.Prefix style) so rollout buckets don't correlate
// across tenants.
func WithSubject(ctx context.Context, id string) context.Context {
	return subjectKey.With(ctx, id)
}

// SubjectFromContext returns the subject ID set by WithSubject.
func SubjectFromContext(ctx context.Context) (string, bool) {
	return subjectKey.From(ctx)
}
```

Replace the Task 3 placeholder `value` in `client.go`:

```go
// value resolves the final string value through the full pipeline:
// lookup → enabled → deny → allow → rollout.
func (c *Client) value(ctx context.Context, key string) (string, bool) {
	id, _ := subjectKey.From(ctx)
	var extra []string
	if c.identity != nil {
		extra = c.identity(ctx)
	}
	return c.valueFor(ctx, key, id, extra)
}

// valueFor is the ctx-carrier-free core shared with Evaluator.
func (c *Client) valueFor(ctx context.Context, key, id string, extra []string) (string, bool) {
	f, ok := c.lookup(ctx, key)
	if !ok || !f.Enabled {
		return "", false
	}
	return eval(f, key, id, extra)
}
```

- [ ] **Step 4: Run the full package tests**

Run: `just fmt ./ops/featureflag/... && go test -race ./ops/featureflag/ -v`
Expected: PASS — including all Task 1-3 tests (pipeline change must not break precedence tests, which all use Rollout 100).

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/eval.go ops/featureflag/subject.go ops/featureflag/client.go ops/featureflag/subject_test.go
git commit -m "feat(featureflag): evaluation pipeline with token targeting and FNV rollout"
```

---

### Task 5: Evaluator — `For(id, tokens...)`

**Files:**
- Modify: `ops/featureflag/subject.go` (append Evaluator)
- Test: `ops/featureflag/subject_test.go` (append)

**Interfaces:**
- Consumes: `Client.valueFor`, coercion helpers from Task 3.
- Produces: `type Evaluator struct` (exported, value type); `func (c *Client) For(id string, tokens ...string) Evaluator`; methods `Bool(key string, def bool) bool`, `String(key, def string) string`, `Int(key string, def int) int`, `Float64(key string, def float64) float64`, `Duration(key string, def time.Duration) time.Duration`.

- [ ] **Step 1: Write the failing tests (append to subject_test.go)**

```go
func TestEvaluator(t *testing.T) {
	t.Parallel()

	c := newClient(t, featureflag.WithFlags(featureflag.Flags{
		"rollout50": {Value: "true", Enabled: true, Rollout: 50},
		"vip_only":  {Value: "42", Enabled: true, Rollout: 0, Allow: []string{"segment:vip"}},
	}))

	t.Run("equivalent to ctx carrier", func(t *testing.T) {
		t.Parallel()
		for i := range 200 {
			id := fmt.Sprintf("usr_%d", i)
			viaCtx := c.Bool(featureflag.WithSubject(context.Background(), id), "rollout50", false)
			viaFor := c.For(id).Bool("rollout50", false)
			assert.Equal(t, viaCtx, viaFor, "id %s", id)
		}
	})

	t.Run("explicit tokens substitute for identity resolver", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 42, c.For("usr_1", "segment:vip").Int("vip_only", 0))
		assert.Equal(t, 0, c.For("usr_1").Int("vip_only", 0))
	})

	t.Run("all typed getters", func(t *testing.T) {
		t.Parallel()
		e := newClient(t,
			featureflag.WithString("s", "x"),
			featureflag.WithFloat64("f", 2.5),
			featureflag.WithDuration("d", time.Minute),
		).For("usr_1")
		assert.Equal(t, "x", e.String("s", ""))
		assert.InDelta(t, 2.5, e.Float64("f", 0), 1e-9)
		assert.Equal(t, time.Minute, e.Duration("d", 0))
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ops/featureflag/ -run TestEvaluator -v`
Expected: FAIL to build — `For` undefined.

- [ ] **Step 3: Append Evaluator to subject.go**

```go
// Evaluator is a subject-bound view of a Client for code without request
// context (jobs, CLIs). Explicit tokens substitute for the identity resolver.
type Evaluator struct {
	client *Client
	id     string
	tokens []string
}

// For binds a subject ID and optional identity tokens.
func (c *Client) For(id string, tokens ...string) Evaluator {
	return Evaluator{client: c, id: id, tokens: slices.Clone(tokens)}
}

// Bool returns the flag coerced to bool, or def on any miss.
func (e Evaluator) Bool(key string, def bool) bool {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseBool(s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "bool")
		return def
	}
	return v
}

// String returns the flag value, or def on any miss.
func (e Evaluator) String(key, def string) string {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	return s
}

// Int returns the flag coerced to int, or def on any miss.
func (e Evaluator) Int(key string, def int) int {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseInt[int](s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "int")
		return def
	}
	return v
}

// Float64 returns the flag coerced to float64, or def on any miss.
func (e Evaluator) Float64(key string, def float64) float64 {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseFloat[float64](s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "float64")
		return def
	}
	return v
}

// Duration returns the flag coerced to time.Duration, or def on any miss.
func (e Evaluator) Duration(key string, def time.Duration) time.Duration {
	s, ok := e.client.valueFor(context.Background(), key, e.id, e.tokens)
	if !ok {
		return def
	}
	v, err := typeconv.ParseDuration(s)
	if err != nil {
		e.client.warnCoerce(context.Background(), key, s, "duration")
		return def
	}
	return v
}
```

(Add `"slices"`, `"time"`, and the typeconv import to subject.go.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./ops/featureflag/... && go test -race ./ops/featureflag/ -run TestEvaluator -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/subject.go ops/featureflag/subject_test.go
git commit -m "feat(featureflag): For(id, tokens...) bound Evaluator"
```

---

### Task 6: `Cached` provider decorator

**Files:**
- Create: `ops/featureflag/cached.go`
- Test: `ops/featureflag/cached_test.go`

**Interfaces:**
- Consumes: `Provider`, `Lister`, `Flag`, `core/clock.Clock`/`clock.NewMock`, `resilience/singleflight.Group[V].Do(ctx, key, fn) (V, bool, error)`.
- Produces: `func Cached(p Provider, ttl time.Duration, opts ...CacheOption) Provider` (panics on nil p with ErrNilProvider message); `type CacheOption func(*cachedConfig)`; `func CacheKey(scope func(ctx context.Context) string) CacheOption`; `func CacheClock(clk clock.Clock) CacheOption`. Behavior: scope-aware nested-map cache (zero-alloc hit), singleflight refresh, serve-stale-on-error, misses cached (negative caching), entries live for process lifetime, Lister passthrough (uncached) when the wrapped provider implements it.

- [ ] **Step 1: Write the failing tests**

```go
// ops/featureflag/cached_test.go
package featureflag_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/featureflag"
)

// countingProvider counts underlying calls; optionally errors, optionally blocks.
type countingProvider struct {
	mu    sync.Mutex
	flags map[string]featureflag.Flags // scope → flags
	err   error
	calls atomic.Int64
	gate  chan struct{} // when non-nil, Flag blocks until closed
}

var scopeCtx = ctxkey.New[string]("test.scope")

func (p *countingProvider) Flag(ctx context.Context, key string) (featureflag.Flag, bool, error) {
	p.calls.Add(1)
	if p.gate != nil {
		<-p.gate
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return featureflag.Flag{}, false, p.err
	}
	scope, _ := scopeCtx.From(ctx)
	f, ok := p.flags[scope][key]
	return f, ok, nil
}

func scopeOf(ctx context.Context) string { s, _ := scopeCtx.From(ctx); return s }

func TestCached(t *testing.T) {
	t.Parallel()
	enabled := func(v string) featureflag.Flag {
		return featureflag.Flag{Value: v, Enabled: true, Rollout: 100}
	}

	t.Run("hit within TTL skips provider", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		for range 10 {
			f, ok, err := c.Flag(t.Context(), "f")
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, "x", f.Value)
		}
		assert.EqualValues(t, 1, p.calls.Load())
	})

	t.Run("TTL expiry refreshes", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		_, _, _ = c.Flag(t.Context(), "f")
		p.mu.Lock()
		p.flags[""]["f"] = enabled("y")
		p.mu.Unlock()
		mock.Advance(31 * time.Second)
		f, _, _ := c.Flag(t.Context(), "f")
		assert.Equal(t, "y", f.Value)
		assert.EqualValues(t, 2, p.calls.Load())
	})

	t.Run("misses are cached", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		for range 5 {
			_, ok, err := c.Flag(t.Context(), "missing")
			require.NoError(t, err)
			assert.False(t, ok)
		}
		assert.EqualValues(t, 1, p.calls.Load())
	})

	t.Run("serve stale on refresh error", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		_, _, _ = c.Flag(t.Context(), "f")
		p.mu.Lock()
		p.err = assert.AnError
		p.mu.Unlock()
		mock.Advance(31 * time.Second)
		f, ok, err := c.Flag(t.Context(), "f")
		require.NoError(t, err, "stale value served, error swallowed")
		require.True(t, ok)
		assert.Equal(t, "x", f.Value)
	})

	t.Run("cold error propagates", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{err: assert.AnError}
		c := featureflag.Cached(p, 30*time.Second)
		_, _, err := c.Flag(t.Context(), "f")
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("scope isolation", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{
			"tenant_a": {"f": enabled("a")},
			"tenant_b": {"f": enabled("b")},
		}}
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheKey(scopeOf))
		ctxA := scopeCtx.With(t.Context(), "tenant_a")
		ctxB := scopeCtx.With(t.Context(), "tenant_b")
		fa, _, _ := c.Flag(ctxA, "f")
		fb, _, _ := c.Flag(ctxB, "f")
		assert.Equal(t, "a", fa.Value)
		assert.Equal(t, "b", fb.Value)
		// both cached independently
		_, _, _ = c.Flag(ctxA, "f")
		_, _, _ = c.Flag(ctxB, "f")
		assert.EqualValues(t, 2, p.calls.Load())
	})

	t.Run("singleflight collapses concurrent misses", func(t *testing.T) {
		t.Parallel()
		gate := make(chan struct{})
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}, gate: gate}
		c := featureflag.Cached(p, 30*time.Second)
		var wg sync.WaitGroup
		for range 20 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, _ = c.Flag(context.Background(), "f")
			}()
		}
		time.Sleep(50 * time.Millisecond) // let goroutines pile onto the flight
		close(gate)
		wg.Wait()
		assert.EqualValues(t, 1, p.calls.Load(), "one provider call for 20 concurrent readers")
	})

	t.Run("lister passthrough", func(t *testing.T) {
		t.Parallel()
		mem := featureflag.NewMemory(featureflag.Flags{"f": enabled("x")})
		c := featureflag.Cached(mem, time.Second)
		l, ok := c.(featureflag.Lister)
		require.True(t, ok, "Cached over a Lister must expose All")
		all, err := l.All(t.Context())
		require.NoError(t, err)
		assert.Len(t, all, 1)

		plain := &countingProvider{flags: map[string]featureflag.Flags{"": {}}}
		_, isLister := featureflag.Cached(plain, time.Second).(featureflag.Lister)
		assert.False(t, isLister, "Cached over a non-Lister must not fake All")
	})

	t.Run("nil provider panics", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { featureflag.Cached(nil, time.Second) })
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ops/featureflag/ -run TestCached -v`
Expected: FAIL to build — `Cached` undefined.

- [ ] **Step 3: Write cached.go**

```go
// ops/featureflag/cached.go
package featureflag

import (
	"context"
	"sync"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/singleflight"
)

// CacheOption configures Cached.
type CacheOption func(*cachedConfig)

type cachedConfig struct {
	scope func(ctx context.Context) string
	clk   clock.Clock
}

// CacheKey sets the scope function partitioning cache entries — for
// multi-tenant providers return the tenant ID from ctx. Default scope is ""
// (single-tenant). Cache cardinality is scopes × flags, never users.
func CacheKey(scope func(ctx context.Context) string) CacheOption {
	return func(c *cachedConfig) { c.scope = scope }
}

// CacheClock injects a clock (tests). Default: clock.System().
func CacheClock(clk clock.Clock) CacheOption {
	return func(c *cachedConfig) { c.clk = clk }
}

// Cached wraps a Provider with scope-aware TTL caching: singleflight
// refresh (one loader per entry regardless of concurrent readers),
// serve-stale-on-error (a failed refresh serves the last-known value — the
// failure mode is "yesterday's flags", not "everything off"), and negative
// caching of misses. Entries live for the process lifetime; memory stays
// bounded because cardinality is scopes × flags.
//
// If p implements Lister, the returned Provider does too (All passes
// through uncached — it is an admin/debug path).
//
// Cached panics if p is nil (programmer error, mirrors ErrNilProvider).
func Cached(p Provider, ttl time.Duration, opts ...CacheOption) Provider {
	if p == nil {
		panic(ErrNilProvider.Error())
	}
	cfg := cachedConfig{
		scope: func(context.Context) string { return "" },
		clk:   clock.System(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	c := &cached{p: p, ttl: ttl, cfg: cfg, data: map[string]map[string]cacheEntry{}}
	if l, ok := p.(Lister); ok {
		return &cachedLister{cached: c, lister: l}
	}
	return c
}

type cacheEntry struct {
	at   time.Time
	flag Flag
	ok   bool
}

type cached struct {
	p    Provider
	cfg  cachedConfig
	data map[string]map[string]cacheEntry // scope → key → entry
	sf   singleflight.Group[cacheEntry]
	mu   sync.RWMutex
	ttl  time.Duration
}

func (c *cached) Flag(ctx context.Context, key string) (Flag, bool, error) {
	scope := c.cfg.scope(ctx)
	c.mu.RLock()
	e, hit := c.data[scope][key]
	c.mu.RUnlock()
	if hit && c.cfg.clk.Now().Sub(e.at) < c.ttl {
		return e.flag, e.ok, nil
	}
	fresh, _, err := c.sf.Do(ctx, scope+"\x00"+key, func(ctx context.Context) (cacheEntry, error) {
		f, ok, err := c.p.Flag(ctx, key)
		if err != nil {
			return cacheEntry{}, err
		}
		ne := cacheEntry{flag: f, ok: ok, at: c.cfg.clk.Now()}
		c.mu.Lock()
		m := c.data[scope]
		if m == nil {
			m = map[string]cacheEntry{}
			c.data[scope] = m
		}
		m[key] = ne
		c.mu.Unlock()
		return ne, nil
	})
	if err != nil {
		if hit {
			return e.flag, e.ok, nil // serve stale
		}
		return Flag{}, false, err
	}
	return fresh.flag, fresh.ok, nil
}

type cachedLister struct {
	*cached
	lister Lister
}

func (c *cachedLister) All(ctx context.Context) (Flags, error) {
	return c.lister.All(ctx)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `just fmt ./ops/featureflag/... && go test -race ./ops/featureflag/ -run TestCached -v`
Expected: PASS, no races.

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/cached.go ops/featureflag/cached_test.go
git commit -m "feat(featureflag): Cached decorator with singleflight and serve-stale"
```

---

### Task 7: `Client.All` merged listing

**Files:**
- Modify: `ops/featureflag/client.go` (append All)
- Test: `ops/featureflag/client_test.go` (append)

**Interfaces:**
- Consumes: `Lister`, provider chain, `staticProvider.All`.
- Produces: `func (c *Client) All(ctx context.Context) (Flags, error)` — merges across Lister providers in chain order, first-hit-wins per key; non-Lister providers skipped; per-provider errors joined but partial results returned.

- [ ] **Step 1: Write the failing test (append to client_test.go)**

```go
func TestClientAll(t *testing.T) {
	t.Parallel()

	t.Run("merges listers with chain precedence", func(t *testing.T) {
		t.Parallel()
		mem := featureflag.NewMemory(featureflag.Flags{
			"shared": {Value: "mem", Enabled: true, Rollout: 100},
			"only_mem": {Value: "m", Enabled: true, Rollout: 100},
		})
		c, err := featureflag.New(
			featureflag.WithProvider(mem),
			featureflag.WithString("shared", "static"),
			featureflag.WithString("only_static", "s"),
		)
		require.NoError(t, err)
		all, err := c.All(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "mem", all["shared"].Value, "earlier provider wins")
		assert.Equal(t, "m", all["only_mem"].Value)
		assert.Equal(t, "s", all["only_static"].Value)
	})

	t.Run("non-lister providers are skipped", func(t *testing.T) {
		t.Parallel()
		plain := &fakeProvider{flags: featureflag.Flags{"invisible": {Value: "x", Enabled: true, Rollout: 100}}}
		c, err := featureflag.New(featureflag.WithProvider(plain), featureflag.WithBool("visible", true))
		require.NoError(t, err)
		all, err := c.All(t.Context())
		require.NoError(t, err)
		assert.NotContains(t, all, "invisible")
		assert.Contains(t, all, "visible")
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ops/featureflag/ -run TestClientAll -v`
Expected: FAIL to build — `All` undefined on `*Client`.

- [ ] **Step 3: Append All to client.go**

```go
// All merges the flag sets of every provider implementing Lister, in chain
// order with first-hit-wins per key (matching evaluation precedence).
// Providers without Lister are skipped. Per-provider errors are joined into
// the returned error while partial results are still returned. Debug/admin
// visibility only — not an evaluation path.
func (c *Client) All(ctx context.Context) (Flags, error) {
	out := Flags{}
	var errs []error
	for _, p := range c.providers {
		l, ok := p.(Lister)
		if !ok {
			continue
		}
		fs, err := l.All(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for k, f := range fs {
			if _, exists := out[k]; !exists {
				out[k] = f
			}
		}
	}
	return out, errors.Join(errs...)
}
```

(Add `"errors"` to client.go imports.)

- [ ] **Step 4: Run all package tests**

Run: `just fmt ./ops/featureflag/... && go test -race ./ops/featureflag/ -v`
Expected: PASS (entire package).

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/client.go ops/featureflag/client_test.go
git commit -m "feat(featureflag): Client.All merged listing across Lister providers"
```

---

### Task 8: doc.go + packages.md

**Files:**
- Create: `ops/featureflag/doc.go`
- Modify: `docs/packages.md` (three edits: tree comment ~line 182, catalog ops section ~line 525/564, recipes owed ~line 696)

**Interfaces:** none new — documentation of everything above. The doc content below is the deliverable; write it verbatim (trim only if a referenced API drifted during implementation).

- [ ] **Step 1: Write doc.go**

```go
// Package featureflag provides standalone feature flags: bool/variant values
// with typed getters, deterministic percentage rollout, token-based
// allow/deny targeting, and pluggable flag storage behind a one-method
// Provider seam. A flag is a small serializable data record; all evaluation
// (enabled → deny → allow → rollout) happens in this package, so every
// backend — config file, memory, your own database — behaves identically.
//
// # Micro-SaaS floor (config only)
//
// Embed Flags in the app config (ops/config), one option, done:
//
//	// config/production.yaml:
//	//   flags:
//	//     dark_mode: true
//	//     new_editor: { value: true, rollout: 25 }
//	type AppConfig struct {
//		Flags featureflag.Flags `yaml:"flags"`
//	}
//
//	flags, err := featureflag.New(featureflag.WithFlags(cfg.Flags))
//	if flags.Bool(r.Context(), "new_editor", false) { /* new path */ }
//
// Getters never error: missing key, disabled flag, provider failure, lost
// rollout bucket, or a malformed value all return the caller's default
// (WithLogger surfaces the latter two as WARN records).
//
// # Subjects, rollout, and identity tokens
//
// Percentage rollout buckets on a stable subject ID set once per request by
// auth middleware; identity tokens make populations targetable in flag data:
//
//	ctx = featureflag.WithSubject(ctx, user.ID) // IDs should be globally unique (id.Prefix style)
//
//	flags, _ := featureflag.New(
//		featureflag.WithFlags(cfg.Flags),
//		// tokens must be O(1) from pre-loaded request state — never a DB call;
//		// compute segment membership at session create/refresh
//		featureflag.WithIdentity(func(ctx context.Context) []string {
//			return session.From(ctx).Tokens // ["role:staff", "segment:vip"]
//		}),
//	)
//
// A flag with rollout 0 and allow [role:staff] is the dogfooding pattern:
// live in production for the team, invisible to users. Raising a rollout
// percent never flips existing subjects off (FNV bucketing is monotonic).
// Jobs and CLIs without request context bind the subject explicitly:
//
//	if flags.For(job.UserID, "segment:vip").Bool("new_payout", false) { ... }
//
// # Runtime toggles without infrastructure
//
// The Memory provider is mutable — a guarded admin handler makes a
// maintenance gate (the recipe formerly owed by docs/packages.md):
//
//	mem := featureflag.NewMemory(nil)
//	flags, _ := featureflag.New(featureflag.WithProvider(mem), featureflag.WithFlags(cfg.Flags))
//
//	// admin handler behind auth/guard:
//	_ = mem.Set("maintenance", featureflag.Flag{Value: "true", Enabled: true, Rollout: 100})
//
//	// middleware:
//	if flags.Bool(r.Context(), "maintenance", false) {
//		problem.ServiceUnavailable("maintenance in progress").Write(w, r)
//		return
//	}
//
// # Database-backed flags (multi-tenant recipe)
//
// A Provider is one method; ctx carries the tenant. Wrap it in Cached —
// cache cardinality is tenants × flags, never users, and a failed refresh
// serves the last-known value (singleflight collapses refresh stampedes):
//
//	// CREATE TABLE feature_flags (
//	//     tenant_id     text        NOT NULL,
//	//     key           text        NOT NULL,
//	//     value         text        NOT NULL DEFAULT 'true',
//	//     enabled       boolean     NOT NULL DEFAULT true,
//	//     rollout       smallint    NOT NULL DEFAULT 100 CHECK (rollout BETWEEN 0 AND 100),
//	//     allow_tokens  text[]      NOT NULL DEFAULT '{}',
//	//     deny_tokens   text[]      NOT NULL DEFAULT '{}',
//	//     has_overrides boolean     NOT NULL DEFAULT false,
//	//     updated_at    timestamptz NOT NULL DEFAULT now(),
//	//     PRIMARY KEY (tenant_id, key)
//	// );
//	// -- individual force on/off at scale, consulted only when has_overrides:
//	// CREATE TABLE feature_flag_overrides (
//	//     tenant_id text NOT NULL, key text NOT NULL, subject_id text NOT NULL,
//	//     allow boolean NOT NULL, PRIMARY KEY (tenant_id, key, subject_id)
//	// );
//
//	type pgFlags struct{ db *pgxpool.Pool }
//
//	func (p *pgFlags) Flag(ctx context.Context, key string) (featureflag.Flag, bool, error) {
//		var f featureflag.Flag
//		err := p.db.QueryRow(ctx,
//			`SELECT value, enabled, rollout, allow_tokens, deny_tokens
//			   FROM feature_flags WHERE tenant_id = $1 AND key = $2`,
//			tenant.From(ctx), key,
//		).Scan(&f.Value, &f.Enabled, &f.Rollout, &f.Allow, &f.Deny)
//		if errors.Is(err, pgx.ErrNoRows) {
//			return featureflag.Flag{}, false, nil // miss → next layer (config defaults)
//		}
//		return f, err == nil, err
//	}
//
//	flags, _ := featureflag.New(
//		featureflag.WithProvider(featureflag.Cached(&pgFlags{db}, 30*time.Second,
//			featureflag.CacheKey(func(ctx context.Context) string { return tenant.From(ctx) }))),
//		featureflag.WithFlags(cfg.Flags), // platform defaults under the DB layer
//		featureflag.WithIdentity(identityTokens),
//	)
//
// Discipline: tokens for populations (segment:vip), the overrides table for
// individuals, array columns only for handfuls. Tenant-facing flag
// management (a catalog gating which flags operators may touch, validated
// CRUD over these tables) is an application feature — this package only
// reads; see the design doc for the full back-office recipe.
//
// # Providers and precedence
//
// New's option order is the lookup chain, first hit wins; the static set
// occupies the position of the first static option. A provider error is
// logged and treated as a miss for that provider, so a database outage
// degrades to config defaults, not to hardcoded call-site defaults.
// Client.All merges Lister providers with the same precedence for a debug
// endpoint or admin page.
package featureflag
```

- [ ] **Step 2: Verify examples compile conceptually and package builds**

Run: `just fmt ./ops/featureflag/... && go vet ./ops/featureflag/ && go build ./ops/featureflag/`
Expected: clean. (Doc examples are comments — verify identifier names against the shipped API by grepping: `grep -o 'featureflag\.[A-Za-z]*' ops/featureflag/doc.go | sort -u` and check each exists in the package.)

- [ ] **Step 3: Update docs/packages.md (three edits)**

Edit 1 — tree comment in the `ops/` block (~line 179-183): move `featureflag` from planned to shipped:

```
│   # shipped: supervisor logger (logger/sentry) config health
│   #          buildinfo automaxprocs logredact bootstrap featureflag
│   # planned: debug metrics (metrics/prometheus)
│   #          auditlog (auditlog/pgsink) cli
```

Edit 2 — catalog `### ops/` section: append `featureflag` to the `Shipped:` line with a one-line parenthetical (matching how config/health/bootstrap are annotated), and DELETE the planned bullet `- **featureflag** — recommended. ...`:

```
Shipped: supervisor logger (logger/sentry) config health buildinfo
automaxprocs logredact bootstrap featureflag. (…existing parentheticals…
`featureflag` — standalone flags as serializable records (enabled → deny →
allow → rollout pipeline, token-set targeting, FNV subject bucketing): typed
getters with defaults, YAML/options/memory sources behind a one-method store
Provider seam (ctx-scoped for multi-tenancy), scope-aware Cached decorator
(singleflight, serve-stale). Postgres provider is a doc.go recipe.)
```

Edit 3 — "Recipes owed" section (~line 696): remove `maintenance 503 gate (featureflag + problem) ·` — it now lives in the package's doc.go.

- [ ] **Step 4: Full package test + lint checkpoint**

Run: `just fmt ./ops/featureflag/... && just lint && just test ./ops/featureflag/...`
Expected: all green. Fix any modernize/betteralign/nilaway findings now (betteralign may reorder struct fields — accept its changes).

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/doc.go docs/packages.md
git commit -m "docs(featureflag): package documentation; mark shipped in packages.md"
```

---

### Task 9: Benchmarks + performance acceptance loop

**Files:**
- Create: `ops/featureflag/bench_test.go`

**Interfaces:** consumes the full public API. Acceptance criteria from the spec:
- Static/cached hit path: **0 allocs/op**, ≤ ~300 ns/op (Apple Silicon dev machine, order of magnitude).
- Rollout + token-match paths: ≤ 1 alloc/op, sub-microsecond.
- Parallel variants: no throughput collapse under 8+ goroutines.
- The loop exits when targets are met OR a remaining gap has a written justification recorded for the PR description.

- [ ] **Step 1: Write bench_test.go**

```go
// ops/featureflag/bench_test.go
package featureflag_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/featureflag"
)

func benchClient(b *testing.B, opts ...featureflag.Option) *featureflag.Client {
	b.Helper()
	c, err := featureflag.New(opts...)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

func BenchmarkBool_StaticHit(b *testing.B) {
	c := benchClient(b, featureflag.WithBool("f", true))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if !c.Bool(ctx, "f", false) {
			b.Fatal("expected true")
		}
	}
}

func BenchmarkBool_StaticHit_Parallel(b *testing.B) {
	c := benchClient(b, featureflag.WithBool("f", true))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = c.Bool(ctx, "f", false)
		}
	})
}

func BenchmarkBool_Rollout(b *testing.B) {
	c := benchClient(b,
		featureflag.WithBool("f", true),
		featureflag.WithRollout("f", 50),
	)
	ctx := featureflag.WithSubject(context.Background(), "usr_2a9f8c31")
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Bool(ctx, "f", false)
	}
}

func BenchmarkBool_TokenMatch(b *testing.B) {
	c := benchClient(b,
		featureflag.WithBool("f", true),
		featureflag.WithRollout("f", 0),
		featureflag.WithAllow("f", "segment:vip", "role:staff"),
		featureflag.WithIdentity(func(context.Context) []string {
			return []string{"role:support", "segment:vip"}
		}),
	)
	ctx := featureflag.WithSubject(context.Background(), "usr_2a9f8c31")
	b.ReportAllocs()
	for b.Loop() {
		if !c.Bool(ctx, "f", false) {
			b.Fatal("expected allow match")
		}
	}
}

func BenchmarkBool_Miss(b *testing.B) {
	mem := featureflag.NewMemory(nil)
	c := benchClient(b,
		featureflag.WithProvider(mem),
		featureflag.WithBool("other", true),
	)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Bool(ctx, "nope", false)
	}
}

func BenchmarkString_Coerce(b *testing.B) {
	c := benchClient(b, featureflag.WithString("s", "banner text"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = c.String(ctx, "s", "")
	}
}

func BenchmarkDuration_Coerce(b *testing.B) {
	c := benchClient(b, featureflag.WithDuration("d", 5*time.Second))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Duration(ctx, "d", 0)
	}
}

func BenchmarkCached_Hit(b *testing.B) {
	slow := featureflag.NewMemory(featureflag.Flags{
		"f": {Value: "true", Enabled: true, Rollout: 100},
	})
	c := benchClient(b, featureflag.WithProvider(
		featureflag.Cached(slow, time.Hour, featureflag.CacheClock(clock.System())),
	))
	ctx := context.Background()
	_ = c.Bool(ctx, "f", false) // warm
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Bool(ctx, "f", false)
	}
}

func BenchmarkCached_Hit_Parallel(b *testing.B) {
	slow := featureflag.NewMemory(featureflag.Flags{
		"f": {Value: "true", Enabled: true, Rollout: 100},
	})
	c := benchClient(b, featureflag.WithProvider(featureflag.Cached(slow, time.Hour)))
	_ = c.Bool(context.Background(), "f", false) // warm
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = c.Bool(ctx, "f", false)
		}
	})
}

func BenchmarkFor_Evaluator(b *testing.B) {
	c := benchClient(b,
		featureflag.WithBool("f", true),
		featureflag.WithRollout("f", 50),
	)
	e := c.For("usr_2a9f8c31")
	b.ReportAllocs()
	for b.Loop() {
		_ = e.Bool("f", false)
	}
}
```

- [ ] **Step 2: Run the benchmarks**

Run: `just bench ./ops/featureflag/...`
Expected output shape (numbers will vary):

```
BenchmarkBool_StaticHit-10          	xxxxxxx	     nn ns/op	      0 B/op	      0 allocs/op
BenchmarkBool_StaticHit_Parallel-10 	...
```

- [ ] **Step 3: THE ACCEPTANCE LOOP — analyze, improve, repeat**

Analyze against the criteria. Known likely offenders and their fixes:

| Symptom | Likely cause | Fix |
|---|---|---|
| allocs on StaticHit | logger attr construction on non-warn path, or interface boxing in `lookup` | ensure warn paths construct `slog.Attr` only after the error check (already structured that way); check `subjectKey.From` isn't forcing an alloc |
| allocs on Cached_Hit | string concat in cache key on the HIT path | hits must use only the nested-map read (`data[scope][key]`); concat belongs exclusively inside the singleflight refresh |
| allocs on TokenMatch | identity resolver slice is caller-supplied (1 alloc is the resolver's, acceptable) | only chase allocs originating inside the package |
| Parallel collapse | `RWMutex` contention in Cached | acceptable if within ~2× single-thread per-op cost; if worse, switch `cached.data` to `atomic.Pointer[map[...]]` copy-on-write |
| Duration_Coerce slow | `ParseDuration` per call | acceptable — documented cost of variant flags; do NOT add per-call memoization (YAGNI) |

Loop protocol (repeat until exit):
1. `just bench ./ops/featureflag/...` — record full output.
2. Compare each benchmark against the criteria table above.
3. If all pass → exit loop, go to Step 4.
4. If a miss is fixable: apply fix, `just test ./ops/featureflag/...` to prove no regression, go to 1.
5. If a miss is fundamental (e.g., resolver-owned alloc): write a one-line justification; if every remaining miss has a justification → exit loop.

- [ ] **Step 4: Record results**

Save the final benchmark output — it goes verbatim into the PR description in Task 11. Write it to `/tmp` is forbidden; use the session scratchpad or keep it in the commit message body.

- [ ] **Step 5: Commit**

```bash
git add ops/featureflag/bench_test.go
git commit -m "test(featureflag): benchmarks with performance acceptance results

<paste final benchmark table here>"
```

---

### Task 10: Full verification sweep

**Files:** none new.

- [ ] **Step 1: Format, lint, race-test the whole module**

```bash
just fmt ./ops/featureflag/...
just lint
just test ./ops/featureflag/...
just test   # full module — proves no cross-package breakage from packages.md/API
```

Expected: everything green. Fix any finding (nilaway on the `cached_test` gate channel, betteralign struct reorders, modernize `new(expr)` suggestions) and re-run until clean. Note: if `just lint` reports golangci cache weirdness from a sibling worktree, run `golangci-lint cache clean` and retry.

- [ ] **Step 2: Verify test coverage of spec behaviors**

Run: `go test -race -cover ./ops/featureflag/`
Expected: coverage ≥ 85%. If a spec behavior (see spec "Testing" section) lacks a test, add it now.

- [ ] **Step 3: Commit any fixes**

```bash
git add -A ops/featureflag docs/packages.md
git commit -m "chore(featureflag): lint and coverage sweep"  # only if changes exist
```

---

### Task 11: Autonomous PR delivery loop (no user interaction)

**Files:** none — GitHub workflow. **This task loops until ALL of: CI green, Claude review issues fixed, all review threads resolved.** Never ask the user anything; fix everything in-session.

- [ ] **Step 1: Push and create the PR**

```bash
git push -u origin claude/nervous-chatterjee-91d06f
gh pr create --base main \
  --title "feat(ops): featureflag package" \
  --body "$(cat <<'EOF'
## Summary
Ships `ops/featureflag` per docs/superpowers/specs/2026-07-08-featureflag-design.md:
- Serializable `Flag` records; YAML scalar-or-object config via ops/config (no yaml.v3 import — legacy func-style unmarshaler)
- Evaluation pipeline: enabled → deny → allow → rollout; token-set targeting ({subjectID} ∪ WithIdentity(ctx)); inline FNV-1a bucketing (deterministic, monotonic ramps)
- Typed getters with inline defaults (never error, fail-safe)
- One-method store `Provider` seam + `Lister` upgrade; layered providers, first hit wins; mutable `Memory`; scope-aware `Cached` (singleflight, serve-stale-on-error, negative caching)
- `WithSubject` carrier + `For(id, tokens...)` evaluator; Postgres multi-tenant recipe in doc.go
- packages.md: featureflag moved to shipped; maintenance-gate recipe now lives in doc.go

## Benchmarks
<paste final benchmark table from Task 9>

## Test plan
- Black-box suite: YAML matrix, pipeline semantics, rollout determinism/monotonicity/distribution/decorrelation, provider precedence, Cached TTL/singleflight/stale/scope isolation, concurrency under -race
EOF
)"
```

Constraint check before running: body contains NO attribution lines.

- [ ] **Step 2: CI loop — repeat until every check passes**

```bash
gh pr checks --watch   # blocks until checks settle
```

- If any check fails: `gh run view <run-id> --log-failed` → diagnose (use superpowers:systematic-debugging for non-obvious failures) → fix → `just fmt ./ops/featureflag/... && just lint && just test ./ops/featureflag/...` locally → commit → push → re-run `gh pr checks --watch`.
- Known trap: a `.env`-named test fixture would be silently dropped by .gitignore (verify with `git archive` if CI can't find a file that exists locally). This plan has no such fixtures, but new ones must be force-added.
- Do not proceed to Step 3 until ALL checks are green.

- [ ] **Step 3: Claude review loop — repeat until zero unresolved threads**

1. Wait for the `claude-code-review` workflow to complete (it runs on PR open/sync):
   `gh pr checks --watch` again, then fetch the review:
   ```bash
   gh pr view --json reviews,comments --jq '.reviews[-1].body, (.comments[] | .body)'
   gh api "repos/{owner}/{repo}/pulls/$(gh pr view --json number -q .number)/comments" --jq '.[] | {path, line, body}'
   ```
2. **Timeout fallback:** the review workflow is known to time out on large PRs, post nothing, and still "pass". If 10 minutes after CI green there is no review body, run a self-review instead: dispatch the `pr-review-toolkit:code-reviewer` agent on the full branch diff (`git diff main...HEAD`) and treat its findings as the review.
3. For EACH finding: verify it against the code first (do not blindly implement — some review suggestions are wrong; check with the superpowers:receiving-code-review mindset). Real issue → fix + test proving the fix; incorrect finding → prepare a short technical rebuttal.
4. After all fixes: `just fmt ./ops/featureflag/... && just lint && just test ./ops/featureflag/...` → commit → push.
5. Reply to and resolve every thread:
   ```bash
   # list unresolved threads
   gh api graphql -f query='query($owner:String!,$repo:String!,$pr:Int!){
     repository(owner:$owner,name:$repo){pullRequest(number:$pr){
       reviewThreads(first:50){nodes{id isResolved comments(first:1){nodes{body path line}}}}}}}' \
     -f owner='{owner}' -f repo='{repo}' -F pr=<num>
   # for each fixed thread: reply with what was done, then resolve
   gh api graphql -f query='mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{isResolved}}}' -f id=<thread-id>
   ```
   Replies state what changed (commit SHA) or the rebuttal. No attribution lines in comments.
6. Pushing fixes re-triggers CI and possibly a fresh review → **go back to Step 2**. Loop terminates only when simultaneously: all checks green AND the latest review has no unaddressed findings AND `reviewThreads` shows zero `isResolved: false`.

- [ ] **Step 4: Final state report**

Post-loop, produce the session summary: PR URL, final CI status, benchmark table, count of review findings fixed/rebutted. Do NOT merge — merging is the user's call when they wake up.

---

## Self-Review (performed while writing this plan)

- **Spec coverage:** data model + YAML (T1), Provider/Memory (T2), options/New/precedence (T3), pipeline/subject/identity/rollout (T4), Evaluator (T5), Cached (T6), Client.All (T7), doc.go recipes incl. pg schema + tenant self-service pointer + maintenance gate, packages.md edits (T8), benchmarks + acceptance loop (T9), lint/coverage (T10), autonomous PR delivery (T11). Spec's `SubjectFromContext` wasn't in the spec API list but is required by app-side identity resolvers (test `fromCtx` demonstrates why) — additive, consistent with ctxkey conventions.
- **Placeholder scan:** none — all steps carry complete code/commands.
- **Type consistency:** `Flag` field order matches betteralign expectations; `valueFor(ctx, key, id, extra)` signature identical in Tasks 4/5; `CacheClock`/`CacheKey` names consistent across Tasks 6/9.
