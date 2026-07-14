# auth/access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `auth/access`, the framework's authorization decision seam — a three-valued `Decider` interface with generic combinators, stdlib-only built-in deciders, a 403 middleware, and a typed load-then-authorize handler.

**Architecture:** A stdlib-only decision core (`Subject`/`Action`/`Resource` vocabulary, three-valued `Decision`, `Decider` interface, `FirstDecisive`/`DenyOverrides` combinators, built-in `ScopeDecider`/`TenantMatch`/`AllowAll`/`DenyAll`) plus a thin middleware layer (`RequirePermission`/`Require` and the generic `Model[T].Handle`) that consumes `guard` identity in and writes `problem` 403s out. `access` owns no storage and never queries anything: resource attributes are caller-supplied.

**Tech Stack:** Go 1.26, stdlib (`context`, `slices`, `net/http`, `log/slog`); forge seams `core/ctxkey`, `auth/guard`, `web/middleware`, `web/problem`. Module path `github.com/dmitrymomot/forge`.

## Global Constraints

- **Package path:** `auth/access`, package name `access`, import `github.com/dmitrymomot/forge/auth/access`.
- **Tests:** black-box only (`package access_test`); test doubles are the package's own `DeciderFunc`/`AllowAll`/`DenyAll` — no external mocking.
- **Idiom:** `type Option func(*config)`, never builders. Anatomy: `doc.go` · `access.go` · `combinators.go` · `deciders.go` · `middleware.go` · `model.go` · `options.go` · `context.go` · `errors.go`.
- **Dependency policy:** the decision core (`access.go`, `combinators.go`, `deciders.go`) imports **stdlib + `auth/guard` only** (`SubjectFromIdentity`); `net/http`, `core/ctxkey`, `web/middleware`, `web/problem` appear only in the middleware side (`middleware.go`, `model.go`, `options.go`, `context.go`).
- **Fail closed:** any `Abstain` that reaches a terminal, any missing subject, and any decider error resolve to `Deny` → HTTP **403** (never 401 — that split is guard's). Client bodies never leak the decision reason; the full `Decision` rides context for a custom responder / `auditlog`.
- **Tenancy:** no `WithScope` seam; tenancy is `Subject.Tenant`/`Resource.Tenant` data plus the `TenantMatch` built-in. Single-tenant apps leave both empty and pay nothing.
- **Perf:** the `Decide` path through `FirstDecisive` + `ScopeDecider`/`TenantMatch` is **zero-alloc** (gated by a test). Built-in deciders use **constant** reason strings. `SubjectFromIdentity` copies scalars + the `Scopes` slice header only and leaves `Attrs` nil (no per-request `Meta` map allocation). The middleware/`Model` paths are *not* zero-alloc — `r.WithContext` for the decision stash allocates by design; benchmark them but do not gate at zero.
- **After each file change:** `just fmt ./auth/access/...` (runs gofmt, goimports, betteralign-apply — do not hand-tune struct field order; betteralign reorders it). **After the final task:** `just lint`.

---

### Task 1: Decision core — vocabulary, seam, `Authorize`

**Files:**
- Create: `auth/access/access.go`
- Create: `auth/access/context.go`
- Test: `auth/access/access_test.go`

**Interfaces:**
- Consumes: `guard.Identity{Subject, Tenant, Scopes []string, Meta map[string]string, Method}` from `github.com/dmitrymomot/forge/auth/guard`; `ctxkey.New[T](name) Key[T]` with `.With(ctx,v)`/`.From(ctx)(T,bool)` from `github.com/dmitrymomot/forge/core/ctxkey`.
- Produces:
  - `type Subject struct { ID, Tenant string; Scopes []string; Attrs map[string]any }`
  - `type Action string`
  - `type Resource struct { Type, ID, Tenant string; Attrs map[string]any }`
  - `type Effect uint8` with `Abstain Effect = iota; Allow; Deny`, `func (Effect) String() string`, `func (Effect) Because(reason string) Decision`
  - `type Decision struct { Effect Effect; Decider, Reason string; Trace []Decision }`
  - `type Decider interface { Decide(context.Context, Subject, Action, Resource) (Decision, error) }`
  - `type DeciderFunc func(context.Context, Subject, Action, Resource) (Decision, error)` (implements `Decider`)
  - `func Named(name string, d Decider) Decider`
  - `func SubjectFromIdentity(id guard.Identity) Subject`
  - `func Authorize(ctx context.Context, d Decider, s Subject, a Action, r Resource) (Decision, error)`
  - `func DecisionFrom(ctx context.Context) (Decision, bool)` (in `context.go`)

- [ ] **Step 1: Write the failing test**

Create `auth/access/access_test.go`:

```go
package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/guard"
)

func TestEffectBecause(t *testing.T) {
	d := access.Allow.Because("ok")
	if d.Effect != access.Allow || d.Reason != "ok" {
		t.Fatalf("got %+v", d)
	}
}

func TestDeciderFuncImplementsDecider(t *testing.T) {
	var d access.Decider = access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Allow.Because("yes"), nil
	})
	got, err := d.Decide(context.Background(), access.Subject{}, "act", access.Resource{})
	if err != nil || got.Effect != access.Allow {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestNamedStampsEmptyDeciderOnly(t *testing.T) {
	inner := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Allow.Because("r"), nil // Decider left empty
	})
	got, _ := access.Named("role", inner).Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if got.Decider != "role" {
		t.Fatalf("want stamped role, got %q", got.Decider)
	}

	preset := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{Effect: access.Deny, Decider: "acl", Reason: "r"}, nil
	})
	got, _ = access.Named("role", preset).Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if got.Decider != "acl" {
		t.Fatalf("want preserved acl, got %q", got.Decider)
	}
}

func TestSubjectFromIdentity(t *testing.T) {
	id := guard.Identity{Subject: "u1", Tenant: "t1", Scopes: []string{"a", "b"}, Meta: map[string]string{"email": "x"}}
	s := access.SubjectFromIdentity(id)
	if s.ID != "u1" || s.Tenant != "t1" || len(s.Scopes) != 2 {
		t.Fatalf("got %+v", s)
	}
	if s.Attrs != nil {
		t.Fatalf("Attrs must stay nil (no Meta promotion), got %v", s.Attrs)
	}
}

func TestAuthorizeAllowPassthrough(t *testing.T) {
	d := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Allow.Because("ok"), nil
	})
	got, err := access.Authorize(context.Background(), d, access.Subject{}, "a", access.Resource{})
	if err != nil || got.Effect != access.Allow {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestAuthorizeAbstainBecomesDeny(t *testing.T) {
	d := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, nil // Abstain
	})
	got, err := access.Authorize(context.Background(), d, access.Subject{}, "a", access.Resource{})
	if err != nil || got.Effect != access.Deny || got.Decider != "access" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestAuthorizeErrorFailsClosed(t *testing.T) {
	sentinel := errors.New("store down")
	d := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, sentinel
	})
	got, err := access.Authorize(context.Background(), d, access.Subject{}, "a", access.Resource{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
	if got.Effect != access.Deny {
		t.Fatalf("want fail-closed Deny, got %+v", got)
	}
}

func TestDecisionFromRoundTrip(t *testing.T) {
	if _, ok := access.DecisionFrom(context.Background()); ok {
		t.Fatal("empty ctx must report ok=false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/access/...`
Expected: build failure / FAIL — `access` package does not exist yet.

- [ ] **Step 3: Write minimal implementation**

Create `auth/access/access.go`:

```go
// Package access is the authorization decision seam: it answers "can this
// subject do this action on this resource?" behind a three-valued Decider
// interface that rbac/acl/abac implement and guard's RequirePermission
// middleware consumes (the 403 half of the 401-vs-403 split). access owns no
// storage and never queries anything; resource attributes are caller-supplied.
package access

import (
	"context"

	"github.com/dmitrymomot/forge/auth/guard"
)

// Subject is the actor a decision is made about. It is its own type rather
// than a reuse of guard.Identity so the seam works off the HTTP hot path too
// (a background job has no Identity). Attrs is the abac attribute bag.
type Subject struct {
	Scopes []string       // token scopes; ScopeDecider reads these
	Attrs  map[string]any // abac reads these; nil-safe
	ID     string         // principal id
	Tenant string         // optional tenant scope
}

// Action is a verb-on-noun permission string, e.g. "documents:read".
type Action string

// Resource is the thing acted upon. Attrs are caller-supplied — access never
// fetches them. ID == "" means a type-level / collection check.
type Resource struct {
	Attrs  map[string]any
	Type   string
	ID     string
	Tenant string // enables the built-in cross-tenant veto (TenantMatch)
}

// Effect is a three-valued authorization outcome. The zero value Abstain
// ("no opinion") lets a layer decline so a lower-precedence layer can decide.
type Effect uint8

const (
	Abstain Effect = iota // no opinion — fall through
	Allow
	Deny
)

// String renders the effect for logs and traces.
func (e Effect) String() string {
	switch e {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	default:
		return "abstain"
	}
}

// Because builds a Decision carrying this effect and a human reason, keeping
// custom deciders terse: `return access.Allow.Because("admin role"), nil`.
func (e Effect) Because(reason string) Decision {
	return Decision{Effect: e, Reason: reason}
}

// Decision is the explanation record for one authorization outcome. Decider
// names the layer that spoke; Reason says why. Trace holds every consulted
// layer's decision and is populated only when the context enables it
// (WithExplain); it is nil on the hot path.
type Decision struct {
	Trace   []Decision
	Decider string
	Reason  string
	Effect  Effect
}

// Decider answers the authorization question. A non-nil error is fail-closed:
// combinators and Authorize turn it into Deny. Implementations must be safe
// for concurrent use.
type Decider interface {
	Decide(ctx context.Context, s Subject, a Action, r Resource) (Decision, error)
}

// DeciderFunc adapts a function to Decider — the package's test fake and the
// shape rbac/acl/abac deciders and ad-hoc consumer predicates take.
type DeciderFunc func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error)

// Decide implements Decider.
func (f DeciderFunc) Decide(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
	return f(ctx, s, a, r)
}

// Named wraps d so its decisions carry name in Decision.Decider when the
// wrapped decider left it empty. Built-in deciders name themselves; Named
// labels ad-hoc closures for the explanation record.
func Named(name string, d Decider) Decider {
	return DeciderFunc(func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
		dec, err := d.Decide(ctx, s, a, r)
		if dec.Decider == "" {
			dec.Decider = name
		}
		return dec, err
	})
}

// SubjectFromIdentity adapts a guard.Identity into a Subject. It is zero-alloc:
// it copies ID, Tenant, and the Scopes slice header (shared, read-only) and
// leaves Attrs nil. guard.Identity.Meta is NOT promoted into Attrs — that
// would allocate a map on every request and the common scope/tenant checks
// never read it. Consumers needing identity metadata in predicates populate
// Attrs themselves via a WithSubject resolver.
func SubjectFromIdentity(id guard.Identity) Subject {
	return Subject{ID: id.Subject, Tenant: id.Tenant, Scopes: id.Scopes}
}

// Authorize runs d.Decide and closes it fail-closed: Abstain becomes a Deny
// ("no layer granted"); a decider error becomes a Deny with the error returned
// separately so in-handler callers may answer 503 instead of 403. The returned
// Decision is always decisive (Allow or Deny).
func Authorize(ctx context.Context, d Decider, s Subject, a Action, r Resource) (Decision, error) {
	dec, err := d.Decide(ctx, s, a, r)
	if err != nil {
		return Decision{Effect: Deny, Decider: firstNonEmpty(dec.Decider, "access"), Reason: err.Error(), Trace: dec.Trace}, err
	}
	if dec.Effect == Abstain {
		return Decision{Effect: Deny, Decider: "access", Reason: "no layer granted", Trace: dec.Trace}, nil
	}
	return dec, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
```

Create `auth/access/context.go`:

```go
package access

import (
	"context"

	"github.com/dmitrymomot/forge/core/ctxkey"
)

var decisionKey = ctxkey.New[Decision]("access")

// DecisionFrom returns the Decision the middleware or Model recorded for this
// request, for auditlog and downstream handlers (and custom responders on the
// deny path). ok is false when no decision was stashed.
func DecisionFrom(ctx context.Context) (Decision, bool) {
	return decisionKey.From(ctx)
}

func withDecision(ctx context.Context, d Decision) context.Context {
	return decisionKey.With(ctx, d)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./auth/access/...` then `just test ./auth/access/...`
Expected: PASS (all `TestEffect*`, `TestDeciderFunc*`, `TestNamed*`, `TestSubjectFromIdentity`, `TestAuthorize*`, `TestDecisionFromRoundTrip`).

- [ ] **Step 5: Commit**

```bash
git add auth/access/access.go auth/access/context.go auth/access/access_test.go
git commit -m "feat(access): decision vocabulary, Decider seam, and Authorize"
```

---

### Task 2: Combinators — `FirstDecisive`, `DenyOverrides`, `WithExplain`

**Files:**
- Create: `auth/access/combinators.go`
- Test: `auth/access/combinators_test.go`

**Interfaces:**
- Consumes: `Decider`, `DeciderFunc`, `Decision`, `Effect` (Task 1).
- Produces:
  - `func FirstDecisive(deciders ...Decider) Decider`
  - `func DenyOverrides(deciders ...Decider) Decider`
  - `func WithExplain(ctx context.Context) context.Context`

- [ ] **Step 1: Write the failing test**

Create `auth/access/combinators_test.go`:

```go
package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
)

func constDecider(name string, e access.Effect) access.Decider {
	return access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{Effect: e, Decider: name}, nil
	})
}

func errDecider(name string, err error) access.Decider {
	return access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{Decider: name}, err
	})
}

func decide(t *testing.T, d access.Decider) access.Decision {
	t.Helper()
	got, err := d.Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	return got
}

func TestFirstDecisiveFirstWins(t *testing.T) {
	d := access.FirstDecisive(
		constDecider("acl", access.Abstain),
		constDecider("rbac", access.Allow),
		constDecider("late", access.Deny),
	)
	got := decide(t, d)
	if got.Effect != access.Allow || got.Decider != "rbac" {
		t.Fatalf("got %+v", got)
	}
}

func TestFirstDecisiveDenyBeatsLaterAllow(t *testing.T) {
	d := access.FirstDecisive(
		constDecider("acl", access.Deny),
		constDecider("rbac", access.Allow),
	)
	if got := decide(t, d); got.Effect != access.Deny || got.Decider != "acl" {
		t.Fatalf("got %+v", got)
	}
}

func TestFirstDecisiveAllAbstain(t *testing.T) {
	d := access.FirstDecisive(constDecider("a", access.Abstain), constDecider("b", access.Abstain))
	if got := decide(t, d); got.Effect != access.Abstain {
		t.Fatalf("got %+v", got)
	}
}

func TestFirstDecisiveErrorFailsClosed(t *testing.T) {
	sentinel := errors.New("boom")
	d := access.FirstDecisive(
		constDecider("acl", access.Abstain),
		errDecider("rbac", sentinel),
		constDecider("late", access.Allow),
	)
	got, err := d.Decide(context.Background(), access.Subject{}, "a", access.Resource{})
	if !errors.Is(err, sentinel) || got.Effect != access.Deny {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestDenyOverridesVetoRegardlessOfOrder(t *testing.T) {
	d := access.DenyOverrides(
		constDecider("rbac", access.Allow),
		constDecider("acl", access.Deny), // later, still vetoes
	)
	if got := decide(t, d); got.Effect != access.Deny || got.Decider != "acl" {
		t.Fatalf("got %+v", got)
	}
}

func TestDenyOverridesFirstAllowWhenNoDeny(t *testing.T) {
	d := access.DenyOverrides(
		constDecider("a", access.Abstain),
		constDecider("b", access.Allow),
		constDecider("c", access.Allow),
	)
	if got := decide(t, d); got.Effect != access.Allow || got.Decider != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestWithExplainPopulatesTrace(t *testing.T) {
	d := access.FirstDecisive(
		constDecider("acl", access.Abstain),
		constDecider("rbac", access.Allow),
	)
	ctx := access.WithExplain(context.Background())
	got, _ := d.Decide(ctx, access.Subject{}, "a", access.Resource{})
	if len(got.Trace) != 2 {
		t.Fatalf("want trace of 2, got %d: %+v", len(got.Trace), got.Trace)
	}
	if got.Trace[0].Decider != "acl" || got.Trace[1].Decider != "rbac" {
		t.Fatalf("trace order wrong: %+v", got.Trace)
	}
}

func TestNoTraceWithoutExplain(t *testing.T) {
	d := access.FirstDecisive(constDecider("acl", access.Allow))
	if got := decide(t, d); got.Trace != nil {
		t.Fatalf("trace must be nil without WithExplain, got %+v", got.Trace)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/access/...`
Expected: build failure — `FirstDecisive`, `DenyOverrides`, `WithExplain` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `auth/access/combinators.go`:

```go
package access

import "context"

type explainKey struct{}

// WithExplain marks ctx so combinators accumulate the full per-layer trace
// into Decision.Trace. Off by default so the hot path allocates no trace slice.
// RequirePermission/Model expose WithExplain() to enable it for a debug request
// or an "explain" endpoint.
func WithExplain(ctx context.Context) context.Context {
	return context.WithValue(ctx, explainKey{}, true)
}

func explaining(ctx context.Context) bool {
	v, _ := ctx.Value(explainKey{}).(bool)
	return v
}

// FirstDecisive returns the first decider's Allow or Deny; Abstain falls
// through. All-abstain returns Abstain. Passing [TenantMatch(), acl, abac,
// rbac] reproduces the documented precedence exactly. A decider error stops
// evaluation and returns a fail-closed Deny plus the wrapped error.
func FirstDecisive(deciders ...Decider) Decider {
	return DeciderFunc(func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
		trace := explaining(ctx)
		var acc []Decision
		for _, d := range deciders {
			dec, err := d.Decide(ctx, s, a, r)
			if trace {
				acc = append(acc, dec)
			}
			if err != nil {
				return Decision{Effect: Deny, Decider: firstNonEmpty(dec.Decider, "access"), Reason: err.Error(), Trace: acc}, err
			}
			if dec.Effect != Abstain {
				dec.Trace = acc
				return dec, nil
			}
		}
		return Decision{Effect: Abstain, Trace: acc}, nil
	})
}

// DenyOverrides evaluates every decider: any Deny vetoes regardless of order;
// otherwise the first Allow wins; otherwise Abstain. A decider error stops
// evaluation and returns a fail-closed Deny plus the wrapped error.
func DenyOverrides(deciders ...Decider) Decider {
	return DeciderFunc(func(ctx context.Context, s Subject, a Action, r Resource) (Decision, error) {
		trace := explaining(ctx)
		var acc []Decision
		var firstAllow Decision
		haveAllow := false
		for _, d := range deciders {
			dec, err := d.Decide(ctx, s, a, r)
			if trace {
				acc = append(acc, dec)
			}
			if err != nil {
				return Decision{Effect: Deny, Decider: firstNonEmpty(dec.Decider, "access"), Reason: err.Error(), Trace: acc}, err
			}
			switch dec.Effect {
			case Deny:
				dec.Trace = acc
				return dec, nil
			case Allow:
				if !haveAllow {
					firstAllow = dec
					haveAllow = true
				}
			}
		}
		if haveAllow {
			firstAllow.Trace = acc
			return firstAllow, nil
		}
		return Decision{Effect: Abstain, Trace: acc}, nil
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./auth/access/...` then `just test ./auth/access/...`
Expected: PASS (all `TestFirstDecisive*`, `TestDenyOverrides*`, `TestWithExplain*`, `TestNoTraceWithoutExplain`).

- [ ] **Step 5: Commit**

```bash
git add auth/access/combinators.go auth/access/combinators_test.go
git commit -m "feat(access): FirstDecisive and DenyOverrides combinators with opt-in trace"
```

---

### Task 3: Built-in deciders — `ScopeDecider`, `TenantMatch`, `AllowAll`, `DenyAll`

**Files:**
- Create: `auth/access/deciders.go`
- Test: `auth/access/deciders_test.go`

**Interfaces:**
- Consumes: `Decider`, `DeciderFunc`, `Decision`, `Effect`, `Subject`, `Action`, `Resource` (Task 1).
- Produces:
  - `func ScopeDecider() Decider`
  - `func TenantMatch() Decider`
  - `func AllowAll() Decider`
  - `func DenyAll(reason string) Decider`

- [ ] **Step 1: Write the failing test**

Create `auth/access/deciders_test.go`:

```go
package access_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
)

func run(t *testing.T, d access.Decider, s access.Subject, a access.Action, r access.Resource) access.Decision {
	t.Helper()
	got, err := d.Decide(context.Background(), s, a, r)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	return got
}

func TestScopeDeciderAllowsWhenScopePresent(t *testing.T) {
	s := access.Subject{Scopes: []string{"documents:read", "documents:write"}}
	if got := run(t, access.ScopeDecider(), s, "documents:read", access.Resource{}); got.Effect != access.Allow {
		t.Fatalf("got %+v", got)
	}
}

func TestScopeDeciderAbstainsWhenScopeAbsent(t *testing.T) {
	s := access.Subject{Scopes: []string{"documents:read"}}
	if got := run(t, access.ScopeDecider(), s, "documents:write", access.Resource{}); got.Effect != access.Abstain {
		t.Fatalf("got %+v", got)
	}
}

func TestTenantMatchDeniesCrossTenant(t *testing.T) {
	s := access.Subject{Tenant: "t1"}
	r := access.Resource{Tenant: "t2"}
	if got := run(t, access.TenantMatch(), s, "a", r); got.Effect != access.Deny || got.Decider != "tenant" {
		t.Fatalf("got %+v", got)
	}
}

func TestTenantMatchAbstainsSameTenant(t *testing.T) {
	s := access.Subject{Tenant: "t1"}
	r := access.Resource{Tenant: "t1"}
	if got := run(t, access.TenantMatch(), s, "a", r); got.Effect != access.Abstain {
		t.Fatalf("got %+v", got)
	}
}

func TestTenantMatchAbstainsWhenEitherEmpty(t *testing.T) {
	// single-tenant app: both empty -> abstain (zero ceremony)
	if got := run(t, access.TenantMatch(), access.Subject{}, "a", access.Resource{}); got.Effect != access.Abstain {
		t.Fatalf("both empty: got %+v", got)
	}
	// resource not tenant-scoped -> abstain
	if got := run(t, access.TenantMatch(), access.Subject{Tenant: "t1"}, "a", access.Resource{}); got.Effect != access.Abstain {
		t.Fatalf("resource empty: got %+v", got)
	}
}

func TestTerminals(t *testing.T) {
	if got := run(t, access.AllowAll(), access.Subject{}, "a", access.Resource{}); got.Effect != access.Allow {
		t.Fatalf("AllowAll got %+v", got)
	}
	got := run(t, access.DenyAll("nope"), access.Subject{}, "a", access.Resource{})
	if got.Effect != access.Deny || got.Reason != "nope" {
		t.Fatalf("DenyAll got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/access/...`
Expected: build failure — `ScopeDecider`, `TenantMatch`, `AllowAll`, `DenyAll` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `auth/access/deciders.go`:

```go
package access

import (
	"context"
	"slices"
)

// ScopeDecider allows when the subject's Scopes contain the action string
// verbatim, else abstains. Exact match only — wildcard/prefix grants are rbac's
// job. This is the day-one usable path: put permissions in token scopes.
// Reasons are constant so the Decide path stays zero-alloc.
func ScopeDecider() Decider {
	return DeciderFunc(func(_ context.Context, s Subject, a Action, _ Resource) (Decision, error) {
		if slices.Contains(s.Scopes, string(a)) {
			return Decision{Effect: Allow, Decider: "scope", Reason: "action in scopes"}, nil
		}
		return Decision{Effect: Abstain, Decider: "scope", Reason: "action not in scopes"}, nil
	})
}

// TenantMatch denies when Subject.Tenant and Resource.Tenant are both set and
// differ; abstains otherwise. Placed first in a chain it makes cross-tenant
// access impossible by construction. Single-tenant apps leave both empty and
// it always abstains.
func TenantMatch() Decider {
	return DeciderFunc(func(_ context.Context, s Subject, _ Action, r Resource) (Decision, error) {
		if s.Tenant != "" && r.Tenant != "" && s.Tenant != r.Tenant {
			return Decision{Effect: Deny, Decider: "tenant", Reason: "cross-tenant access"}, nil
		}
		return Decision{Effect: Abstain, Decider: "tenant", Reason: "same or unscoped tenant"}, nil
	})
}

// AllowAll always allows — a terminal for tests and explicit open policies.
func AllowAll() Decider {
	return DeciderFunc(func(_ context.Context, _ Subject, _ Action, _ Resource) (Decision, error) {
		return Decision{Effect: Allow, Decider: "allow-all", Reason: "allow-all"}, nil
	})
}

// DenyAll always denies with the given reason — a terminal for tests and
// explicit closed policies.
func DenyAll(reason string) Decider {
	return DeciderFunc(func(_ context.Context, _ Subject, _ Action, _ Resource) (Decision, error) {
		return Decision{Effect: Deny, Decider: "deny-all", Reason: reason}, nil
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./auth/access/...` then `just test ./auth/access/...`
Expected: PASS (all `TestScopeDecider*`, `TestTenantMatch*`, `TestTerminals`).

- [ ] **Step 5: Commit**

```bash
git add auth/access/deciders.go auth/access/deciders_test.go
git commit -m "feat(access): built-in ScopeDecider, TenantMatch, and terminals"
```

---

### Task 4: Middleware — options, `RequirePermission`, `Require`

**Files:**
- Create: `auth/access/errors.go`
- Create: `auth/access/options.go`
- Create: `auth/access/middleware.go`
- Test: `auth/access/middleware_test.go`

**Interfaces:**
- Consumes: `Authorize`, `Decision`, `Subject`, `Action`, `Resource`, `withDecision` (Tasks 1–3); `guard.From(ctx) (Identity, bool)`; `middleware.Middleware = func(http.Handler) http.Handler`; `problem.Responder = func(http.ResponseWriter, *http.Request, error)`, `problem.JSON(...Option) Responder`, `problem.WithStatus(int) Option`.
- Produces:
  - `type Option func(*config)`
  - `func WithResource(fn func(*http.Request) Resource) Option`
  - `func WithSubject(fn func(*http.Request) (Subject, bool)) Option`
  - `func WithResponder(p problem.Responder) Option`
  - `func WithLoadError(fn func(http.ResponseWriter, *http.Request, error)) Option`
  - `func WithLogger(l *slog.Logger) Option`
  - `func WithExplain() Option`
  - `func RequirePermission(d Decider, action Action, opts ...Option) middleware.Middleware`
  - `func Require(d Decider, resolve func(*http.Request) (Action, Resource), opts ...Option) middleware.Middleware`
  - (internal) `type config`, `func newConfig(...Option) config`, `func (config) reject(...)`, `func gate(...)`

- [ ] **Step 1: Write the failing test**

Create `auth/access/middleware_test.go`:

```go
package access_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/core/ctxkey"
)

// withIdentity injects a guard.Identity the way guard's middleware would, so
// access's default subject resolver (guard.From) can read it.
var identityKey = ctxkey.New[guard.Identity]("guard")

func reqWithIdentity(id guard.Identity) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(identityKey.With(r.Context(), id))
}

func TestRequirePermissionAllowCallsNext(t *testing.T) {
	called := false
	var seen access.Decision
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		seen, _ = access.DecisionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := access.RequirePermission(access.ScopeDecider(), "documents:read")(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1", Scopes: []string{"documents:read"}}))

	if !called || rr.Code != http.StatusOK {
		t.Fatalf("want next+200, got called=%v code=%d", called, rr.Code)
	}
	if seen.Effect != access.Allow {
		t.Fatalf("decision not stashed: %+v", seen)
	}
}

func TestRequirePermissionDenyGives403(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(access.ScopeDecider(), "documents:write")(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1", Scopes: []string{"documents:read"}}))

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}

func TestRequirePermissionMissingIdentityGives403WithoutNext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true })
	h := access.RequirePermission(access.AllowAll(), "x")(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil)) // no identity

	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 + no next, got called=%v code=%d", called, rr.Code)
	}
}

func TestRequirePermissionDeciderErrorGives403(t *testing.T) {
	boom := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, _ access.Resource) (access.Decision, error) {
		return access.Decision{}, errors.New("store down")
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(boom, "x")(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want fail-closed 403, got %d", rr.Code)
	}
}

func TestWithResourceIsPassedToDecider(t *testing.T) {
	var gotRes access.Resource
	spy := access.DeciderFunc(func(_ context.Context, _ access.Subject, _ access.Action, r access.Resource) (access.Decision, error) {
		gotRes = r
		return access.Allow.Because("ok"), nil
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.RequirePermission(spy, "documents:read",
		access.WithResource(func(_ *http.Request) access.Resource {
			return access.Resource{Type: "document", ID: "42"}
		}),
	)(next)

	h.ServeHTTP(httptest.NewRecorder(), reqWithIdentity(guard.Identity{Subject: "u1"}))
	if gotRes.Type != "document" || gotRes.ID != "42" {
		t.Fatalf("resource not resolved: %+v", gotRes)
	}
}

func TestRequireDynamicAction(t *testing.T) {
	var gotAction access.Action
	spy := access.DeciderFunc(func(_ context.Context, _ access.Subject, a access.Action, _ access.Resource) (access.Decision, error) {
		gotAction = a
		return access.Allow.Because("ok"), nil
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := access.Require(spy, func(_ *http.Request) (access.Action, access.Resource) {
		return "custom:action", access.Resource{Type: "doc"}
	})(next)

	h.ServeHTTP(httptest.NewRecorder(), reqWithIdentity(guard.Identity{Subject: "u1"}))
	if gotAction != "custom:action" {
		t.Fatalf("dynamic action not used: %q", gotAction)
	}
}

func TestWithExplainStashesTrace(t *testing.T) {
	d := access.FirstDecisive(access.TenantMatch(), access.ScopeDecider())
	var seen access.Decision
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = access.DecisionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := access.RequirePermission(d, "documents:read", access.WithExplain())(next)
	h.ServeHTTP(httptest.NewRecorder(), reqWithIdentity(guard.Identity{Subject: "u1", Scopes: []string{"documents:read"}}))
	if len(seen.Trace) == 0 {
		t.Fatalf("want trace stashed under WithExplain, got %+v", seen)
	}
}
```

> Note: `identityKey` here mirrors guard's own unexported context key (`ctxkey.New[guard.Identity]("guard")`). `ctxkey` keys with the same type and name resolve to the same value, so this is how a black-box test seeds what `guard.From` reads. If `guard` exposes a public setter, prefer it; otherwise this mirror is the established test pattern.

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/access/...`
Expected: build failure — `RequirePermission`, `Require`, `WithResource`, `WithExplain` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `auth/access/errors.go`:

```go
package access

import "errors"

// errDenied is the generic error handed to the responder so the client body
// never leaks the decision reason. The full Decision (with reason) rides
// context via DecisionFrom for a custom responder and auditlog.
var errDenied = errors.New("access denied")
```

Create `auth/access/options.go`:

```go
package access

import (
	"log/slog"
	"net/http"

	"github.com/dmitrymomot/forge/auth/guard"
	"github.com/dmitrymomot/forge/web/problem"
)

type config struct {
	resource  func(*http.Request) Resource
	subject   func(*http.Request) (Subject, bool)
	responder problem.Responder
	loadError func(http.ResponseWriter, *http.Request, error)
	logger    *slog.Logger
	explain   bool
}

// Option configures the access middleware and Model handlers.
type Option func(*config)

// WithResource sets how RequirePermission builds the Resource from the request
// (default: the zero Resource — a type-level check). Resolvers stay I/O-free.
func WithResource(fn func(r *http.Request) Resource) Option {
	return func(c *config) {
		if fn != nil {
			c.resource = fn
		}
	}
}

// WithSubject overrides how the Subject is obtained (default: guard.From →
// SubjectFromIdentity). ok=false means fail-closed 403 with no decider call.
func WithSubject(fn func(r *http.Request) (Subject, bool)) Option {
	return func(c *config) {
		if fn != nil {
			c.subject = fn
		}
	}
}

// WithResponder overrides the 403 response (default problem.JSON 403).
func WithResponder(p problem.Responder) Option {
	return func(c *config) {
		if p != nil {
			c.responder = p
		}
	}
}

// WithLoadError overrides the Model load-failure response (default
// problem.JSON 404, which cloaks resource existence).
func WithLoadError(fn func(w http.ResponseWriter, r *http.Request, err error)) Option {
	return func(c *config) {
		if fn != nil {
			c.loadError = fn
		}
	}
}

// WithLogger logs decider errors at Warn (the client still gets a generic 403).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithExplain enables Decision.Trace accumulation for requests through this
// mount (debugging / an "explain" endpoint).
func WithExplain() Option {
	return func(c *config) { c.explain = true }
}

var (
	forbiddenResponder = problem.JSON(problem.WithStatus(http.StatusForbidden))
	notFoundResponder  = problem.JSON(problem.WithStatus(http.StatusNotFound))
)

func defaultSubject(r *http.Request) (Subject, bool) {
	id, ok := guard.From(r.Context())
	if !ok {
		return Subject{}, false
	}
	return SubjectFromIdentity(id), true
}

func newConfig(opts ...Option) config {
	c := config{
		subject:   defaultSubject,
		responder: forbiddenResponder,
		loadError: notFoundResponder,
	}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (c config) reject(w http.ResponseWriter, r *http.Request, cause error) {
	if cause != nil && c.logger != nil {
		c.logger.WarnContext(r.Context(), "access decider error", slog.Any("error", cause))
	}
	c.responder(w, r, errDenied)
}
```

Create `auth/access/middleware.go`:

```go
package access

import (
	"net/http"

	"github.com/dmitrymomot/forge/web/middleware"
)

// RequirePermission gates a route on a static action (the REST common case:
// one route = one action). The Subject comes from guard identity; the Resource
// from the WithResource resolver (default: a type-level check). It answers 403
// on deny/abstain/missing-subject/decider-error and never 401.
func RequirePermission(d Decider, action Action, opts ...Option) middleware.Middleware {
	cfg := newConfig(opts...)
	resolve := func(r *http.Request) (Action, Resource) {
		if cfg.resource != nil {
			return action, cfg.resource(r)
		}
		return action, Resource{}
	}
	return gate(d, cfg, resolve)
}

// Require is the escape hatch for a dynamic action: resolve returns both the
// action and the resource from the request.
func Require(d Decider, resolve func(r *http.Request) (Action, Resource), opts ...Option) middleware.Middleware {
	return gate(d, newConfig(opts...), resolve)
}

func gate(d Decider, cfg config, resolve func(r *http.Request) (Action, Resource)) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if cfg.explain {
				ctx = WithExplain(ctx)
			}
			sub, ok := cfg.subject(r)
			if !ok {
				r = r.WithContext(withDecision(ctx, Decision{Effect: Deny, Decider: "access", Reason: "no subject"}))
				cfg.reject(w, r, nil)
				return
			}
			action, res := resolve(r)
			dec, err := Authorize(ctx, d, sub, action, res)
			r = r.WithContext(withDecision(ctx, dec))
			if dec.Effect != Allow {
				cfg.reject(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./auth/access/...` then `just test ./auth/access/...`
Expected: PASS (all `TestRequirePermission*`, `TestWithResource*`, `TestRequireDynamicAction`, `TestWithExplainStashesTrace`).

- [ ] **Step 5: Commit**

```bash
git add auth/access/errors.go auth/access/options.go auth/access/middleware.go auth/access/middleware_test.go
git commit -m "feat(access): RequirePermission/Require middleware with 403 fail-closed"
```

---

### Task 5: Typed handler — `Model[T]`, `NewModel`

**Files:**
- Create: `auth/access/model.go`
- Test: `auth/access/model_test.go`

**Interfaces:**
- Consumes: `config`, `newConfig`, `withDecision`, `Authorize`, `Decision`, `Decider`, `Action`, `Resource` (Tasks 1, 4).
- Produces:
  - `type Model[T any] struct { Load func(*http.Request) (T, error); Describe func(T) Resource }`
  - `func NewModel[T any](load func(*http.Request) (T, error), describe func(T) Resource) Model[T]`
  - `func (Model[T]) Handle(d Decider, action Action, fn func(http.ResponseWriter, *http.Request, T), opts ...Option) http.Handler`

- [ ] **Step 1: Write the failing test**

Create `auth/access/model_test.go`:

```go
package access_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/auth/guard"
)

type doc struct {
	ID      string
	Tenant  string
	OwnerID string
}

func docModel(load func(*http.Request) (doc, error)) access.Model[doc] {
	return access.NewModel(load, func(d doc) access.Resource {
		return access.Resource{Type: "document", ID: d.ID, Tenant: d.Tenant, Attrs: map[string]any{"owner_id": d.OwnerID}}
	})
}

func TestModelHandleAllowInjectsObject(t *testing.T) {
	m := docModel(func(_ *http.Request) (doc, error) { return doc{ID: "1", OwnerID: "u1"}, nil })
	var got doc
	h := m.Handle(access.AllowAll(), "documents:read", func(w http.ResponseWriter, _ *http.Request, d doc) {
		got = d
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if rr.Code != http.StatusOK || got.ID != "1" {
		t.Fatalf("want injected doc + 200, got code=%d doc=%+v", rr.Code, got)
	}
}

func TestModelHandleDenyGives403(t *testing.T) {
	m := docModel(func(_ *http.Request) (doc, error) { return doc{ID: "1"}, nil })
	called := false
	h := m.Handle(access.DenyAll("no"), "documents:write", func(w http.ResponseWriter, _ *http.Request, _ doc) { called = true })
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if called || rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 + no fn, got called=%v code=%d", called, rr.Code)
	}
}

func TestModelHandleLoadErrorGives404(t *testing.T) {
	loaded := false
	m := docModel(func(_ *http.Request) (doc, error) { loaded = true; return doc{}, errors.New("not found") })
	h := m.Handle(access.AllowAll(), "documents:read", func(w http.ResponseWriter, _ *http.Request, _ doc) {})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, reqWithIdentity(guard.Identity{Subject: "u1"}))
	if !loaded || rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got loaded=%v code=%d", loaded, rr.Code)
	}
}

func TestModelHandleMissingSubjectSkipsLoad(t *testing.T) {
	loaded := false
	m := docModel(func(_ *http.Request) (doc, error) { loaded = true; return doc{}, nil })
	h := m.Handle(access.AllowAll(), "documents:read", func(w http.ResponseWriter, _ *http.Request, _ doc) {})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil)) // no identity
	if loaded || rr.Code != http.StatusForbidden {
		t.Fatalf("want 403 without loading, got loaded=%v code=%d", loaded, rr.Code)
	}
}

func TestNewModelPanicsOnNilFuncs(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic on nil load")
		}
	}()
	access.NewModel[doc](nil, func(d doc) access.Resource { return access.Resource{} })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./auth/access/...`
Expected: build failure — `Model`, `NewModel` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `auth/access/model.go`:

```go
package access

import "net/http"

// Model binds a domain type T to the authorization seam: Load fetches the
// addressed object (consumer code — access never queries) and Describe maps it
// to a Resource. Optional sugar over the core seam.
type Model[T any] struct {
	Load     func(r *http.Request) (T, error)
	Describe func(obj T) Resource
}

// NewModel builds a Model with type inference (T comes from load's return
// type). It panics if either func is nil — a wiring bug caught at startup.
func NewModel[T any](load func(r *http.Request) (T, error), describe func(obj T) Resource) Model[T] {
	if load == nil || describe == nil {
		panic("access: NewModel requires non-nil load and describe")
	}
	return Model[T]{Load: load, Describe: describe}
}

// Handle returns an http.Handler that resolves the Subject (else 403, without
// loading), calls Load (error → 404, cloaks existence), Describes the object,
// authorizes the action (deny → 403; decider error → 403 + logged), stashes the
// Decision, then calls fn with the already-loaded object. WithResource has no
// effect here — Describe supplies the Resource.
func (m Model[T]) Handle(d Decider, action Action, fn func(w http.ResponseWriter, r *http.Request, obj T), opts ...Option) http.Handler {
	cfg := newConfig(opts...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if cfg.explain {
			ctx = WithExplain(ctx)
		}
		sub, ok := cfg.subject(r)
		if !ok {
			r = r.WithContext(withDecision(ctx, Decision{Effect: Deny, Decider: "access", Reason: "no subject"}))
			cfg.reject(w, r, nil)
			return
		}
		obj, err := m.Load(r)
		if err != nil {
			cfg.loadError(w, r, err)
			return
		}
		dec, aerr := Authorize(ctx, d, sub, action, m.Describe(obj))
		r = r.WithContext(withDecision(ctx, dec))
		if dec.Effect != Allow {
			cfg.reject(w, r, aerr)
			return
		}
		fn(w, r, obj)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./auth/access/...` then `just test ./auth/access/...`
Expected: PASS (all `TestModelHandle*`, `TestNewModelPanicsOnNilFuncs`).

- [ ] **Step 5: Commit**

```bash
git add auth/access/model.go auth/access/model_test.go
git commit -m "feat(access): typed Model[T] load-then-authorize handler"
```

---

### Task 6: Docs, runnable example, benchmarks, zero-alloc gate, lint

**Files:**
- Create: `auth/access/doc.go`
- Create: `auth/access/example_test.go`
- Create: `auth/access/bench_test.go`
- Modify: `docs/packages.md` (delete the `auth/access` roadmap entry, lines ~604–609)

**Interfaces:**
- Consumes: the full public API from Tasks 1–5.
- Produces: package documentation, a compiled `Example`, benchmarks, and a zero-alloc gate test.

- [ ] **Step 1: Write the failing test (benchmarks + zero-alloc gate + example)**

Create `auth/access/bench_test.go`:

```go
package access_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/access"
)

func benchInputs() (access.Decider, access.Subject, access.Resource) {
	d := access.FirstDecisive(access.TenantMatch(), access.ScopeDecider())
	s := access.Subject{ID: "u1", Tenant: "t1", Scopes: []string{"documents:read"}}
	r := access.Resource{Type: "document", ID: "42", Tenant: "t1"}
	return d, s, r
}

func BenchmarkFirstDecisiveScope(b *testing.B) {
	d, s, r := benchInputs()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.Decide(ctx, s, "documents:read", r)
	}
}

func TestDecidePathIsZeroAlloc(t *testing.T) {
	d, s, r := benchInputs()
	ctx := context.Background()
	allocs := testing.AllocsPerRun(200, func() {
		_, _ = d.Decide(ctx, s, "documents:read", r)
	})
	if allocs != 0 {
		t.Fatalf("Decide path must be zero-alloc, got %v allocs/op", allocs)
	}
}
```

Create `auth/access/example_test.go`:

```go
package access_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
)

// Example wires the built-in deciders into the documented precedence and
// resolves a decision fail-closed with Authorize.
func Example() {
	decider := access.FirstDecisive(
		access.TenantMatch(),  // cross-tenant → hard deny
		access.ScopeDecider(), // action in token scopes → allow
	)

	sub := access.Subject{ID: "u1", Tenant: "t1", Scopes: []string{"documents:read"}}
	res := access.Resource{Type: "document", ID: "42", Tenant: "t1"}

	dec, _ := access.Authorize(context.Background(), decider, sub, "documents:read", res)
	fmt.Printf("%s by %s\n", dec.Effect, dec.Decider)

	// Cross-tenant is denied by construction.
	other := access.Resource{Type: "document", ID: "99", Tenant: "t2"}
	dec, _ = access.Authorize(context.Background(), decider, sub, "documents:read", other)
	fmt.Printf("%s by %s\n", dec.Effect, dec.Decider)

	// Output:
	// allow by scope
	// deny by tenant
}
```

- [ ] **Step 2: Run test to verify it fails/passes**

Run: `just test ./auth/access/...`
Expected: PASS for `TestDecidePathIsZeroAlloc` and the `Example` (0 allocs; example output matches). If allocs > 0, a built-in decider is building a non-constant reason string — fix it to use a constant before proceeding.

- [ ] **Step 3: Write the package doc**

Create `auth/access/doc.go`:

```go
// Package access is the authorization decision seam — it answers "can this
// subject do this action on this resource?" and is the 403 half of guard's
// 401-vs-403 split (guard authenticates → 401; access authorizes → 403).
//
// The seam is a three-valued Decider: every layer returns Allow, Deny, or
// Abstain ("no opinion"), so layers compose under an explicit precedence.
// FirstDecisive takes the first Allow/Deny and lets Abstain fall through;
// DenyOverrides lets any Deny veto. rbac, acl, and abac will each implement
// Decider and drop into a chain — access never imports them, owns no storage,
// and never fetches a resource; resource attributes are caller-supplied.
//
// Built-in deciders cover the standalone case: ScopeDecider authorizes from
// token scopes, TenantMatch vetoes cross-tenant access, and AllowAll/DenyAll
// are terminals. Every Decision carries an explanation record (which layer
// decided and why) for auditlog and the "why can't this user do X" ticket.
//
// The RequirePermission middleware gates a route on a static action; the
// generic Model[T] handler loads an object, authorizes, and hands the loaded
// object to the business handler — collapsing per-handler ownership boilerplate.
//
// Wiring (behind a guard authn middleware):
//
//	decider := access.FirstDecisive(
//		access.TenantMatch(),
//		access.ScopeDecider(),
//	)
//	read := access.RequirePermission(decider, "documents:read",
//		access.WithResource(func(r *http.Request) access.Resource {
//			return access.Resource{Type: "document", ID: r.PathValue("id"), Tenant: r.PathValue("tenant")}
//		}),
//	)
//	mux.Handle("GET /t/{tenant}/docs/{id}", authn(read(getDoc)))
//
// Resource ownership decided in-handler after the load:
//
//	var docs = access.NewModel(
//		func(r *http.Request) (Document, error) { return store.Load(r.Context(), r.PathValue("id")) },
//		func(d Document) access.Resource {
//			return access.Resource{Type: "document", ID: d.ID, Tenant: d.Tenant, Attrs: map[string]any{"owner_id": d.OwnerID}}
//		},
//	)
//	mux.Handle("PUT /docs/{id}", authn(docs.Handle(editDecider, "documents:write",
//		func(w http.ResponseWriter, r *http.Request, d Document) { /* authorized; d loaded */ })))
package access
```

- [ ] **Step 4: Delete the roadmap entry**

In `docs/packages.md`, remove the `**auth/access**` section (the heading, its paragraph, and its `Deps:` line, plus the surrounding `---` separator) — a shipped package's godoc is its reference, per the repo rule that the roadmap lists only unbuilt packages. Leave the `auth/rbac`, `auth/acl`, `auth/abac` entries (they still say `Deps: auth/access (planned)`; update "(planned)" to nothing only if you want, but that is optional and out of scope here).

- [ ] **Step 5: Full fmt + lint + test**

Run: `just fmt ./auth/access/...`
Run: `just lint`
Run: `just test ./auth/access/...`
Expected: clean vet/build/golangci-lint/nilaway/betteralign/modernize; all tests pass with `-race`.

- [ ] **Step 6: Commit**

```bash
git add auth/access/doc.go auth/access/example_test.go auth/access/bench_test.go docs/packages.md
git commit -m "docs(access): package doc, runnable example, benchmarks, zero-alloc gate"
```

---

## Self-Review

**Spec coverage** (each spec section → task):
- Purpose/role, anti-scope → doc.go (Task 6) + enforced by dependency constraints (Global Constraints).
- Vocabulary (`Subject`/`Action`/`Resource`, `SubjectFromIdentity` zero-alloc no-Meta) → Task 1.
- Decision seam (`Effect`/`Because`/`Decision`/`Decider`/`DeciderFunc`/`Named`) → Task 1.
- `Authorize` → Task 1.
- Combinators + fail-closed error + context-gated trace → Task 2.
- Built-in deciders (`ScopeDecider`/`TenantMatch`/`AllowAll`/`DenyAll`) → Task 3.
- Middleware (`RequirePermission`/`Require`, options, 403-only, missing-subject fail-closed, context stash) → Task 4.
- `Model[T]`/`NewModel` (inject-only handler, load→404, deny→403, subject-absent skips load, nil panic) → Task 5.
- Tenancy (no WithScope; `Subject.Tenant`/`Resource.Tenant` + `TenantMatch`) → Tasks 1, 3.
- File layout → all tasks (matches the spec's list; `Named`/`Authorize` live in `access.go`).
- Perf (zero-alloc Decide gate, constant reasons, no Meta promotion) → Tasks 1, 3, 6. **Deviation from spec §Performance:** the spec listed "the RequirePermission Allow path" as a zero-alloc target; the decision stash (`r.WithContext`) allocates by design, so the gate covers only the `Decide` path (Task 6) and the middleware is benchmarked but not gated. This is captured in Global Constraints.
- Testing policy (black-box, own fakes, all listed cases) → Tasks 1–6.
- rbac/acl/abac plug-in path → documented in doc.go (Task 6).

**Placeholder scan:** no TBD/TODO; every code step carries complete code and exact commands.

**Type consistency:** `Decider.Decide(context.Context, Subject, Action, Resource) (Decision, error)` is identical across Tasks 1–5; `config` fields defined in Task 4 (`resource`/`subject`/`responder`/`loadError`/`logger`/`explain`) are the exact fields read in Tasks 4–5; `withDecision`/`Authorize`/`newConfig`/`reject`/`gate` names match between definition and use; `Model[T]` field names `Load`/`Describe` match `NewModel` and `Handle`.
