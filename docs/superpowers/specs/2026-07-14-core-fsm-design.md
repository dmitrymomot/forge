# core/fsm — Design Spec

Date: 2026-07-14. Status: approved design, pre-implementation.

## Purpose

Typed finite state machine brick: an immutable compiled transition table with guards, hooks, and illegal-transition errors. Pure generics, zero deps, no reflection. Persistence is caller-owned — the machine approves or denies a transition and enriches the entity; the caller writes the status column. Serves two consumer classes with one kernel: compile-time typed lifecycles declared in Go (`finance/invoice`: draft → issued → paid/partially-paid/void/overdue) and runtime-defined flows loaded from tenant configuration (task manager, support-ticket, HR-portal SaaS where each tenant — even each project — draws its own status graph, several flows per tenant).

## Decisions (settled during brainstorming)

1. **Stateless table, caller holds state.** The machine is an immutable declared graph, constructed once and shared; the current state lives in the consumer's DB row and is passed into every call. No instance state, trivially goroutine-safe, cacheable per tenant-flow. No stateful wrapper (YAGNI; ~10 consumer lines if ever wanted).
2. **Target-driven transitions.** The graph is edges `from → to`; firing means naming the target state, matching how status UIs work (kanban drag, status dropdown). No event names in v1; an optional event label on edges is an additive non-breaking change if a consumer ever needs named actions. Two business actions sharing one edge with different rules are dispatched in consumer code.
3. **Typed subject.** `Machine[S ~string, V any]` — `V` is the entity (or entity+actor composite) that guards inspect and hooks mutate. No context smuggling, no `any` assertions.
4. **Guards return `error`, not `bool`** — a denial carries a human-readable reason the UI can show, wrapped so sentinels still match.
5. **Full symmetric attachment surfaces.** Guards and hooks attach to edges and to states (on-enter, on-exit). State-level attachment is what tenant-drawn graphs require: the app declares "entering `done` runs `subtasks_closed`" without knowing which edges the tenant drew into `done`.
6. **Strict construction-time resolution.** `Compile`/`New` fail closed with all issues joined; `Fire` never discovers a definition problem at runtime.
7. **Source-side wildcard.** `"*" → X` (typed: `EdgeFromAny(X)`) expands at construction into concrete edges; pure sugar, zero runtime wildcard logic.

## Package shape

Location `core/fsm`, single package, no subpackages. Files: `doc.go` (runnable example including the compile-and-cache tenant pattern), `machine.go` (Machine, Fire, read API), `define.go` (typed construction: `Define` anchor + parts), `definition.go` (runtime `Definition`/`Registry`/`Compile`), `errors.go`, black-box tests + `bench_test.go`. Estimated ~500–650 implementation LOC. Not an env-configured service: no `config.go`/`options.go` (the `enum` precedent).

## Core types

```go
// Func is the single callback type for guards and hooks.
// Guards are side-effect-free checks; hooks may mutate v. Both abort the transition by returning an error.
type Func[S ~string, V any] func(ctx context.Context, v V, from, to S) error

type Machine[S ~string, V any] struct{ /* immutable after construction */ }
```

`Machine` is immutable after construction and safe for concurrent use. Public methods return exported types only.

## Fire semantics

`Fire(ctx context.Context, v V, from, to S) error`:

1. `from` and `to` must be declared states, else `ErrUnknownState`.
2. An edge `from → to` must exist, else `ErrIllegalTransition` (message carries both states). `from == to` requires an explicit literal self-edge.
3. Guards run in deterministic order — exit-guards(`from`), edge-guards, enter-guards(`to`), each list in declaration order. First error aborts, wrapped as `ErrGuardDenied` + the guard's own error (multi-`%w`).
4. Hooks run only after every guard passed — exit-hooks(`from`), edge-hooks, enter-hooks(`to`), declaration order. First error aborts, wrapped as `ErrHookFailed` + the hook's own error.
5. `nil` return means approved: the caller now persists the new status (and whatever hooks stamped onto `v`).

Everything inside `Fire` happens before persistence. On any error the caller discards the loaded entity — nothing was written, so nothing rolls back; earlier hooks may have mutated the in-memory `v`, which the caller simply drops. Post-persist effects (notifications, outbox events) are explicitly out of scope: consumers run them after their own DB write. A hook must not fire another transition (no cascades).

## Read API

- `Can(from, to S) bool` — structural: edge exists.
- `Next(from S) []S` — structural targets in first-declaration order; nil for an unknown state (mirrors `Can`'s false).
- `Allowed(ctx context.Context, v V, from S) ([]S, error)` — targets whose guards all pass for this entity now; hooks never run; a guard error excludes that target (it is filtering, not failure); error only for an unknown `from` (`ErrUnknownState`).
- `Initial() S`, `States() []S` — declaration order; returned slices are copies.

`Next` renders the generic status dropdown; `Allowed` renders it per-user/per-entity (e.g. a done task offers `open` to admins only, because the `done → open` edge is guarded by `admin_only`). The design idiom: "X is impossible except for role Y" is an existing edge with a guard, never a missing edge.

## Typed construction (compile-time flows)

Go cannot infer `V` through plain option funcs (an edge with no guard never mentions `V`), so construction parts hang off a zero-size type-parameter anchor — one `var` line buys full inference:

```go
var d fsm.Define[Status, *Invoice]

var InvoiceFSM = fsm.MustNew(StatusDraft,
    d.Edge(StatusDraft, StatusIssued, d.Hook(assignNumber)),
    d.Edge(StatusIssued, StatusPaid),
    d.Edge(StatusIssued, StatusPartiallyPaid),
    d.Edge(StatusPartiallyPaid, StatusPaid),
    d.EdgeFromAny(StatusVoid, d.Guard(notPaid)),
    d.OnEnter(StatusIssued, d.Hook(stampIssuedAt)),
)
```

`Define[S, V]` is stateless — not a builder; its methods construct parts that flow into `New(initial S, parts ...Part[S, V]) (*Machine[S, V], error)`. `MustNew` panics, for `var`-init (the `regexp.MustCompile` precedent). `d.Guard`/`d.Hook` produce attachments accepted by `Edge`, `EdgeFromAny`, `OnEnter`, `OnExit`. States are inferred in typed mode: every state mentioned (initial, edge endpoints, wildcard targets, OnEnter/OnExit subjects) is declared; Go constants make an explicit list redundant.

## Runtime construction (tenant-defined flows)

The tenant-facing definition is a plain JSON-tagged struct; consumers store it in their own DB (definition storage is out of scope):

```go
type Definition struct {
    Initial string     `json:"initial"`
    States  []StateDef `json:"states"`
    Edges   []EdgeDef  `json:"edges"`
}

type StateDef struct {
    Name          string   `json:"name"`
    OnEnterGuards []string `json:"on_enter_guards,omitempty"`
    OnEnterHooks  []string `json:"on_enter_hooks,omitempty"`
    OnExitGuards  []string `json:"on_exit_guards,omitempty"`
    OnExitHooks   []string `json:"on_exit_hooks,omitempty"`
}

type EdgeDef struct {
    From   string   `json:"from"` // "*" = any (source-side wildcard)
    To     string   `json:"to"`
    Guards []string `json:"guards,omitempty"`
    Hooks  []string `json:"hooks,omitempty"`
}

type Registry[V any] struct {
    Guards map[string]Func[string, V]
    Hooks  map[string]Func[string, V]
}

func Compile[V any](def Definition, reg Registry[V]) (*Machine[string, V], error)
```

Runtime states have no compile-time identity, so `S` is `string`. The registry is the vocabulary the app exposes to its flow-builder UI; names bind to Go funcs at `Compile`. States are declared explicitly here (unlike typed mode) so typos in edges are caught. A consumer's flow-save endpoint calls `Compile` and surfaces the joined issue list (e.g. 422); a definition that compiles is guaranteed to fire cleanly.

## Wildcard expansion

Expansion happens at construction, after the full state set is known: a wildcard edge to `X` produces a concrete edge from every declared state except `X` itself (wildcards never generate self-loops; self-transitions require an explicit literal self-edge). An explicit edge for a pair fully replaces the expanded one — that is how one source gets extra or different guards. Guards/hooks on the wildcard edge apply to every edge it expands to. Duplicate detection runs on literal `(from, to)` pairs, so two wildcard edges to the same target are duplicates.

## Validation (construction-time, fail closed)

`New` and `Compile` collect all issues via `errors.Join`, wrapped under `ErrInvalidDefinition`, so a flow-builder shows every problem in one save attempt. Issue classes: empty state set; empty or duplicate state name; state named `"*"`; initial missing or undeclared; edge referencing an undeclared state; duplicate edge for a literal `(from, to)` pair (the wildcard `"*"` counts as a literal source here, so two wildcard edges to one target are duplicates); (`Compile` only) guard/hook name absent from the registry. Reachability is deliberately not enforced: a state with no inbound edges is still usable (bulk imports, admin overrides set status outside the FSM); it is a lint concern, not an integrity one.

## Errors

Single-line sentinels in `errors.go`, all `errors.Is`-matchable:

- `ErrInvalidDefinition` — construction failure; wraps the joined issue list.
- `ErrUnknownState` — `Fire`/`Allowed` given a state outside the declared set.
- `ErrIllegalTransition` — no edge `from → to`.
- `ErrGuardDenied` — guard refused; multi-`%w` wraps sentinel + guard error so the domain reason ("3 subtasks still open") survives to the UI.
- `ErrHookFailed` — same double-wrap for hook errors.

`ErrGuardDenied` vs `ErrHookFailed` is the consumer's 4xx/5xx split: user-visible refusal versus something broke mid-transition.

## Tenancy

No storage, no seam: multi-tenancy is which machine you compiled. The doc example shows the pattern — cache compiled machines keyed by `(tenant, flow, version)`, recompile on flow edit; machines are immutable so the cache needs no invalidation beyond the version key. Single-tenant typed use pays zero ceremony (`var InvoiceFSM = fsm.MustNew(...)`). This satisfies the repo tenancy rule by construction: there is no key composition or scoped storage to fail open.

## Performance

`Fire` on a bare edge (no guards/hooks) is the hot path: target 0 allocs/op via a `struct{ from, to S }` map key for edge lookup. `Next`/`States`/`Allowed` allocate (they return copies). Benchmarks: bare `Fire`, `Fire` with guards+hooks, `Allowed` over a fan-out state, `Compile` of a realistic ~10-state/25-edge definition. Post-benchmark optimization pass with before/after numbers in the PR, per repo policy.

## Testing (black-box, `fsm_test`)

- Legality matrix: unknown states, illegal transitions, explicit self-edge allowed, wildcard generates no self-loop, `from == to` without explicit edge fails.
- Ordering: a recording spy proves exit → edge → enter order for guards then hooks, guards-before-any-hook, first-error abort (failing enter-guard ⇒ zero hooks ran).
- Hook mutation on success; abort mid-hooks leaves caller free to discard (documented semantics).
- Wildcard expansion and explicit-edge-replaces-wildcard precedence, including wildcard guards applying to expanded edges.
- Validation matrix: one test per issue class, multi-issue accumulation via `errors.Join`, `errors.Is` matching for every sentinel, guard/hook domain errors surviving the double-wrap.
- `Definition` JSON round-trip; `Allowed` filtering (admin vs non-admin case); concurrent `Fire` on a shared machine under `-race`.

## Anti-scope (restated in doc.go)

No timers, no auto/cascade transitions, no per-instance state or persistence, no flow versioning/migration (recompile per version; migrating in-flight entities is consumer domain), no assignment/roles/SLA semantics, no event labels (additive later), no definition storage, no post-persist effect execution.

## Ship checklist

Delete the `core/fsm` entry from `docs/packages.md` (the `finance/invoice` dep line stays valid as-is). `doc.go` carries the runnable example: typed lifecycle + compile-and-cache tenant flow with named guards.
