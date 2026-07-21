# ops/approval — Maker-Checker Dual Control

Status: approved design, ready for implementation plan.

Catalog entry (`docs/packages.md` → ops/approval): typed approval requests (action + payload) a second person approves or rejects over a storage-agnostic Store; decisions emit `auditlog` events and approver eligibility rides the `auth/access` decision seam. Deps: `auth/access`; `ops/auditlog`.

## Purpose

One sentence: **`ops/approval` records a privileged action, collects the second (or third) person's decision on it, and hands the approved action to exactly one executor.** The two-person rule for payouts, limit overrides, config changes, and manager sign-off.

The package never runs consumer code. It owns the *record* and the *transitions*; the consumer owns the action.

## Scope

In scope:

- Typed request submission (action name + payload), one `Kind[T]` per action type.
- Quorum-based approval, single-reject-is-terminal rejection, cancellation.
- Structural dual-control invariants: no self-approval, one vote per approver.
- Approver eligibility via the `auth/access` decision seam.
- Lazy expiry of unacted requests.
- Execute-once claim/complete/fail lease so an approved action runs once.
- Audit trail via `ops/auditlog`.
- Storage-agnostic `Store` + in-memory implementation + `approval/pgstore`.
- Optional fail-closed tenant scoping.

Out of scope (deliberate):

- Executing the approved action. No handler registry, no dispatch, no retries — that is `async/workflow`/`async/queue` territory. The package's `Execute` helper is a closure wrapper over its own three transitions, not a registry.
- Notification of approvers (email/Slack). Consumers read `List` and notify.
- A background sweeper goroutine. Expiry is lazy; consumers sweep on their own schedule.
- Approval *chains* / multi-stage escalation ("manager, then VP"). A single quorum per kind. Multi-stage is a consumer composing two requests, or a later addition once a real consumer needs it.
- Delegation / out-of-office proxy approvers. Consumer's decider concern.

## Architecture

```
consumer ──Submit[T]──► Manager ──CAS──► Store ──► memory | pgstore
                          │
                          ├──eligibility──► access.Decider   (optional)
                          ├──trail────────► auditlog.Recorder (optional)
                          └──tenant───────► WithScope hook    (optional)
```

`Manager` is non-generic and holds the kind registry, the policies, and the seams. Type safety lives in package-level generic functions (`Submit`, `PayloadOf`, `WithKind`) — the `async/queue` `Kind[T]` precedent. This keeps one Manager per application rather than one per payload type.

### Files

| File | Contents |
| --- | --- |
| `doc.go` | Package doc + runnable example |
| `approval.go` | `Request`, `Decision`, `Status`, `Vote`, `Kind[T]`, `SubmitParams`, `Actor`, `Filter` |
| `policy.go` | `Policy`, kind registry entry, validation |
| `manager.go` | `New`, `Get`, `List`, CAS retry loop, lazy expiry, scope resolution |
| `submit.go` | `Submit[T]`, `PayloadOf[T]` |
| `decide.go` | `Approve`, `Reject`, `Cancel` |
| `execute.go` | `Claim`, `Complete`, `Fail`, `Release`, `Execute` |
| `eligibility.go` | access.Decider invocation and fail-closed mapping |
| `audit.go` | auditlog event emission |
| `store.go` | `Store` interface |
| `memory.go` | In-memory `Store` |
| `options.go` | `type Option func(*config)`, `WithKind`, `WithDecider`, `WithAuditor`, `WithScope`, `WithClock`, `WithMaxRetries` |
| `errors.go` | `errors.Is`-matchable single-line sentinels |
| `bench_test.go` | Required benchmarks (see Performance) |
| `approvaltest/` | Exported Store conformance suite, shared by memory and pgstore |
| `pgstore/` | Postgres driver + goose migrations + integration tests |

Target ~700–850 LOC for the core package, inside the single-responsibility band.

## Types

```go
type Status uint8

const (
    Pending   Status = iota // awaiting decisions
    Approved                // quorum met, awaiting claim (or terminal for the consumer)
    Rejected                // a checker rejected; terminal
    Cancelled               // requester or admin withdrew; terminal
    Expired                 // TTL elapsed while Pending or Approved; terminal (derived)
    Executing               // claimed by an executor
    Executed                // executor reported success; terminal
    Failed                  // executor reported failure; terminal
)

func (s Status) String() string
func (s Status) Terminal() bool // Rejected, Cancelled, Expired, Executed, Failed

type Vote uint8

const (
    VoteApprove Vote = iota + 1
    VoteReject
)
```

`Status` is a `uint8` with a `String()` — not a string type — to keep `Request` small and comparisons cheap. The store persists the numeric value; `pgstore` maps it to a `smallint`.

```go
// Kind binds an approval action name to its payload type T. Declare one
// package-level Kind per action and share it between submitters and approvers.
type Kind[T any] struct{ name string }

func NewKind[T any](name string) Kind[T] // panics on empty name
func (k Kind[T]) Name() string

// Policy is the per-kind rule set. Registered at construction, never
// supplied by the caller at submit time — an attacker who controls the
// submit call must not be able to weaken the gate.
type Policy struct {
    // Quorum is the number of distinct approvals required. Must be >= 1.
    Quorum int
    // TTL is how long a request stays actionable after submission.
    // Zero means it never expires.
    TTL time.Duration
    // ClaimTTL is how long an executor's claim is held before another
    // executor may take it. Zero (default) means the claim never expires:
    // a dead executor wedges the request until Release is called. Setting
    // it non-zero opts into at-least-once execution.
    ClaimTTL time.Duration
}
```

```go
// Request is one privileged action awaiting dual control.
type Request struct {
    CreatedAt time.Time
    ExpiresAt time.Time // zero = never expires
    ClaimedAt time.Time // zero = unclaimed
    DecidedAt time.Time // when it reached a terminal or Approved status

    Decisions []Decision
    Meta      map[string]string
    Payload   json.RawMessage

    Kind      string
    Tenant    string
    Requester string
    ClaimedBy string
    // Reason is the maker's justification, captured at submit and shown to
    // checkers deciding on the request.
    Reason string

    ID      id.UUID
    Version int64
    Status  Status
}

// Decision is one checker's vote. Append-only within a Request.
type Decision struct {
    At       time.Time
    Approver string
    Reason   string
    Vote     Vote
}
```

Field order above is illustrative; `betteralign` (via `just lint`) is authoritative for the final layout.

All timestamps are UTC and **truncated to microseconds** before persistence so records survive a Postgres round-trip unchanged (the `finance/ledger` replay trap — `timestamptz` is microsecond-precision, Go's `time.Time` is nanosecond).

```go
type SubmitParams struct {
    Meta      map[string]string // free-form context, cloned on submit
    Requester string            // required — the maker
    Tenant    string            // optional; must agree with WithScope when set
    Reason    string            // why the action is being requested
}

// Actor is the human acting on a request — the checker casting a decision,
// or the party cancelling. It carries an access.Subject rather than a bare
// id so the decider has real attributes to judge on. Executors are machines
// and pass a plain executor string instead.
type Actor struct {
    Subject access.Subject
    Reason  string
}

type Filter struct {
    // Statuses matches the STORED status. Expired is never stored (it is
    // derived on read), so listing it matches nothing — query
    // Statuses: []Status{Pending, Approved} with ExpiresBefore instead.
    Statuses      []Status
    ExpiresBefore time.Time // zero = no bound
    Kind          string
    Tenant        string
    Requester     string
    Limit         int // 0 = store default
}
```

## API

```go
func New(store Store, opts ...Option) *Manager

// Typed edges.
func Submit[T any](ctx context.Context, m *Manager, k Kind[T], payload T, p SubmitParams) (Request, error)
func PayloadOf[T any](k Kind[T], r Request) (T, error)

// Reads.
func (m *Manager) Get(ctx context.Context, reqID id.UUID) (Request, error)
func (m *Manager) List(ctx context.Context, f Filter) ([]Request, error)

// Decisions.
func (m *Manager) Approve(ctx context.Context, reqID id.UUID, a Actor) (Request, error)
func (m *Manager) Reject(ctx context.Context, reqID id.UUID, a Actor) (Request, error)
func (m *Manager) Cancel(ctx context.Context, reqID id.UUID, a Actor) (Request, error)

// Execute-once lease. Executors are machines, so these take a plain
// executor id rather than an Actor.
func (m *Manager) Claim(ctx context.Context, reqID id.UUID, executor string) (Request, error)
func (m *Manager) Complete(ctx context.Context, reqID id.UUID, executor string) (Request, error)
func (m *Manager) Fail(ctx context.Context, reqID id.UUID, executor, reason string) (Request, error)

// Release is the administrative escape hatch for a request wedged in
// Executing by a dead executor. It is deliberately NOT holder-checked —
// the holder is the party that cannot call it. Consumers gate access to
// it; every call is audited.
func (m *Manager) Release(ctx context.Context, reqID id.UUID, a Actor) (Request, error)

// Execute wraps Claim -> fn -> Complete/Fail so the trio cannot be miswired.
// A non-nil fn error transitions to Failed and is returned joined with any
// transition error. ErrAlreadyClaimed is returned as-is: another executor won.
func (m *Manager) Execute(ctx context.Context, reqID id.UUID, executor string, fn func(context.Context, Request) error) (Request, error)
```

`New` panics on: nil store, duplicate kind name, `Quorum < 1`, negative `TTL`/`ClaimTTL`. These are startup wiring bugs, matching `apikey.New`'s nil-store panic and `guard.New`'s nil-verifier panic.

### Options

```go
func WithKind[T any](k Kind[T], p Policy) Option // accumulating; generic func returning Option
func WithDecider(d access.Decider) Option
func WithAuditor(rec *auditlog.Recorder) Option
func WithScope(fn func(context.Context) (string, error)) Option
func WithClock(c clock.Clock) Option
func WithMaxRetries(n int) Option // CAS retry attempts, default 3
```

`WithKind` is a generic function returning a plain `Option`, so kinds enter `New` as accumulating options rather than a post-construction `Register` method. This follows the `abac` `WithRules` ruling: builder values enter `New` via accumulating options, never a mutating builder on a live object. It also means the registry is immutable after `New` and needs no lock on the read path.

## State machine

```
                    ┌──────────► Rejected      (a single VoteReject; terminal)
                    │
                    ├──────────► Cancelled     (requester/admin; terminal)
                    │
  submit ──► Pending┤
                    │
                    ├──────────► Expired       (TTL elapsed; derived on read)
                    │
                    └─quorum──► Approved
                                   │
                                   ├──► Expired    (TTL elapsed before claim)
                                   │
                                   └─Claim──► Executing ──Complete──► Executed
                                                    │
                                                    ├──Fail─────────► Failed
                                                    │
                                                    └──Release──────► Approved
```

Transition rules:

1. **Votes are legal only from `Pending`.** Any other status → `ErrNotPending`.
2. **A single `Reject` is terminal.** No reject quorum. One checker saying no ends it.
3. **Quorum counts distinct approvers.** The Nth distinct `VoteApprove` sets `Approved` and stamps `DecidedAt`.
4. **`Cancel` is legal from `Pending` and `Approved`.** Not from `Executing` — an in-flight action is not cancellable; use `Fail`. The requester may always cancel their own request; any other actor is gated on `<kind>:cancel` when a decider is set (see Eligibility).
5. **`Claim` is legal only from `Approved`.** From `Executing`, it succeeds only when the existing claim is stale (`ClaimTTL > 0` and `ClaimedAt + ClaimTTL <= now`) — otherwise `ErrAlreadyClaimed`.
6. **`Complete`/`Fail` are legal only from `Executing`** (`ErrNotExecuting` otherwise) **and only for the current `ClaimedBy` holder** (`ErrNotClaimHolder` otherwise).
7. **`Release` is legal only from `Executing`** (`ErrNotExecuting` otherwise) and is **not** holder-checked — it exists precisely for the case where the holder is dead. It returns the request to `Approved` and clears `ClaimedBy`/`ClaimedAt`.

### Structural invariants (not configurable off)

- **No self-approval.** `Actor.Subject.ID == Request.Requester` → `ErrSelfApproval`, for both `Approve` and `Reject`. This holds at `Quorum: 1` too — which is precisely why `Quorum: 1` is still dual control and must be documented as such.
- **One vote per approver.** A second decision from the same `Subject.ID` → `ErrAlreadyVoted`, whichever way they voted the first time.
- **A rejected request never becomes approved.** Guaranteed by rule 1 + rule 2.

## Eligibility

When `WithDecider` is set, every `Approve` and `Reject` first asks the decision seam:

```go
dec, err := access.Authorize(ctx, decider, a.Subject,
    access.Action(r.Kind+":decide"),
    access.Resource{
        Type:   "approval",
        ID:     r.ID.String(),
        Tenant: r.Tenant,
        Attrs: map[string]any{
            "kind":      r.Kind,
            "requester": r.Requester,
            "payload":   r.Payload, // json.RawMessage; decoded only if a rule cares
        },
    })
```

- **One action, `<kind>:decide`, for both votes.** Being a checker is the privilege; which way the checker votes is not a separate grant. Rejection is gated too — an ungated reject lets anyone DoS the approval queue.
- **`Cancel` by a non-requester uses `<kind>:cancel`,** a distinct action: withdrawing someone else's request is a different privilege from judging it, and a policy that grants one need not grant the other. The requester short-circuits this check — withdrawing your own request is never gated.
- **`Attrs` carries `requester` and the raw `payload`.** `requester` enables relational policies ("must be the requester's manager"); the raw payload enables value-aware policies ("over 10 days needs the department head") without forcing a decode on rules that don't care.
- **Fail closed.** `Deny` → `ErrNotEligible`. A decider *error* → `ErrNotEligible` with the underlying error wrapped (`access.Authorize` already collapses errors to Deny). The decider's `Reason` may carry internal detail and must not be echoed to clients verbatim — same warning `access.Decision` documents.
- **The decider runs last.** Check order is: load → expiry → status → self-approval → already-voted → eligibility → CAS. Cheap local invariants reject first; the decider may hit a database or an org chart, so it only runs once the vote is otherwise legal. Every attempt that reaches the decider and is denied is audited.

Without `WithDecider`, any non-requester may vote. That is the correct single-team default and is documented as such: the structural invariants still hold.

## Expiry

Lazy, evaluated on read — no background goroutine.

- `Submit` stamps `ExpiresAt = now + Policy.TTL` (zero TTL → zero `ExpiresAt`, never expires).
- Any load of a `Pending` or `Approved` request whose `ExpiresAt` is non-zero and `<= now` reports `Status: Expired`. Every transition on it fails with `ErrExpired`.
- Expiry is **derived, not written**. The stored row keeps its last written status; the effective status is computed on read. This avoids a write on the read path and keeps expiry correct without a sweeper.
- `Executing` does **not** expire — an in-flight action is governed by `ClaimTTL`, not `TTL`.
- Consumers who want expired rows materialized, or who want to nudge approvers before expiry, use `Filter.ExpiresBefore` on their own schedule.

**`List` semantics with lazy expiry:** `Filter.Statuses` matches the *stored* status in the store query. The Manager applies expiry to each returned record and then drops records whose effective status no longer matches the requested set. Consequence: `List` returns *up to* `Limit` records, and a `Statuses: [Pending]` query may return fewer than the store matched. This is documented on `Filter`.

## Store

```go
// Store persists approval requests. Implementations must be safe for
// concurrent use.
type Store interface {
    // Create persists a new request. ErrDuplicate when the ID exists.
    Create(ctx context.Context, r Request) error

    // Get loads one request. ErrNotFound for unknown ids.
    Get(ctx context.Context, reqID id.UUID) (Request, error)

    // List returns matching requests, newest first (UUIDv7 id order).
    List(ctx context.Context, f Filter) ([]Request, error)

    // Update persists r only when the stored Version equals expect,
    // otherwise ErrConflict. Implementations persist r with Version
    // expect+1. This is the package's only concurrency primitive.
    Update(ctx context.Context, r Request, expect int64) error
}
```

Rationale for whole-record CAS over first-class decision rows: one primitive is the smallest thing a third-party store author can implement correctly, the quorum math stays in the package where it is tested, and "did this vote tip us to Approved" needs its own CAS regardless. Decisions ride a JSON column. Maker-checker volumes are single-digit approvers per request, so the read-modify-write is cheap; the benchmark proves it.

Implementations may normalize nil vs. empty `Decisions`/`Meta` in either direction; callers must not depend on which form comes back.

### CAS retry loop

Every mutating transition is:

```
for attempt := 0; attempt <= maxRetries; attempt++ {
    r := load(ctx, reqID)          // fresh read each attempt
    applyExpiry(&r)
    validate(r)                    // status, self-approval, already-voted — re-checked every attempt
    next := transition(r)
    if err := store.Update(ctx, next, r.Version); errors.Is(err, ErrConflict) {
        continue
    }
    return next, err
}
return Request{}, ErrConflict
```

Re-validating inside the loop is load-bearing: without it, a retry after a lost race could apply a second vote from an approver whose first vote landed in the winning write, or push a request past quorum.

### memory.go

Mutex-guarded `map[id.UUID]Request`. Stores deep copies (`slices.Clone` on `Decisions`, `maps.Clone` on `Meta`) on the way in and out so callers cannot mutate stored state through a returned `Request` — the aliasing bug this package cannot afford. Suitable for tests and single-process dev; not durable.

### pgstore

Table `forge_approval_requests`:

```sql
CREATE TABLE forge_approval_requests (
    id          uuid PRIMARY KEY,
    kind        text NOT NULL,
    tenant      text NOT NULL DEFAULT '',
    requester   text NOT NULL,
    status      smallint NOT NULL,
    version     bigint NOT NULL DEFAULT 1,
    payload     json NOT NULL,
    reason      text NOT NULL DEFAULT '',
    decisions   json NOT NULL DEFAULT '[]'::json,
    meta        jsonb NOT NULL DEFAULT '{}'::jsonb,
    claimed_by  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz,
    claimed_at  timestamptz,
    decided_at  timestamptz
);

CREATE INDEX forge_approval_requests_list_idx
    ON forge_approval_requests (tenant, kind, status, id DESC);

CREATE INDEX forge_approval_requests_expiry_idx
    ON forge_approval_requests (status, expires_at)
    WHERE expires_at IS NOT NULL;
```

- `payload` and `decisions` are `json`, not `jsonb`: byte-exact round-trip, no reparse/rewrite cost, and neither is queried by content (the `async/workflow` state ruling). `meta` is `jsonb` — it is small and consumers may want to index it.
- The list index covers the `Filter` WHERE columns in filter order (the `async/outbox` claim-index lesson: an index that does not cover the filter is not used).
- `Update` is `UPDATE ... SET ..., version = version + 1 WHERE id = $1 AND version = $2`; zero rows affected → distinguish `ErrConflict` from `ErrNotFound` with a follow-up existence check.
- Migrations follow the `apikey/pgstore` shape: `//go:embed migrations/*.sql`, `fs.Sub` rooting, applied under its own version table `forge_approval_schema` (a distinct group name — colliding groups silently skip).

## Tenancy

Standard forge seam. `WithScope(fn)` derives the tenant from context:

- `Submit` stamps the scoped tenant; an explicitly-passed `SubmitParams.Tenant` must be empty or equal to it.
- `Get` and every transition report `ErrNotFound` for other tenants' requests — not `ErrScope`, so the API does not confirm the existence of out-of-tenant records.
- `List` is confined to the scoped tenant.
- Fail closed: a hook error or an empty scoped tenant aborts with `ErrScope`.
- Without the hook, single-tenant use pays zero ceremony and `Tenant` stays `""`.

## Audit

`WithAuditor(rec)` emits one `auditlog.Event` per state-changing call:

| Call | Action | Outcome |
| --- | --- | --- |
| `Submit` | `approval.submit` | success |
| `Approve` | `approval.approve` | success |
| `Reject` | `approval.reject` | success |
| ineligible vote attempt | `approval.approve` / `approval.reject` | **denied** |
| `Cancel` | `approval.cancel` | success |
| ineligible cancel attempt | `approval.cancel` | **denied** |
| `Claim` | `approval.claim` | success |
| `Complete` | `approval.complete` | success |
| `Fail` | `approval.fail` | failure |
| `Release` | `approval.release` | success |

- `Actor` = the voter/executor/requester id. `Resource` = `"approval:<uuid>"`. `Tenant` = the request's tenant. `Meta` carries `kind`, `status` (the resulting effective status), and `quorum`/`approvals` on vote events.
- **Denied attempts are audited.** An ineligible approver trying to push a payout through is the single most security-relevant event this package sees; it must not be invisible.
- **No event on lazily-observed expiry.** Expiry is derived on every read; emitting there would write an audit event per `Get`.
- **Ordering: store update first, then audit.** A failed audit write returns the updated `Request` alongside an `ErrAuditFailed`-wrapped error. The transition is durable; the trail write is not, and the caller can `errors.Is` it to page someone. Auditing before the CAS would record transitions that never happened; swallowing the error would be a silent failure in a compliance package.

## Errors

`errors.go`, single-line `errors.Is`-matchable sentinels:

```
ErrNotFound          ErrDuplicate         ErrConflict
ErrUnknownKind       ErrKindMismatch      ErrRequesterRequired
ErrSelfApproval      ErrAlreadyVoted      ErrNotPending
ErrExpired           ErrNotApproved       ErrAlreadyClaimed
ErrNotClaimHolder    ErrNotExecuting      ErrNotEligible
ErrScope             ErrAuditFailed
```

`ErrNotFound`, `ErrDuplicate`, and `ErrConflict` are the Store contract's sentinels, defined here and returned by every implementation.

## Testing

Black-box (`package approval_test`) throughout; no white-box files anticipated.

- **State machine:** every legal transition, and every illegal one asserted against its specific sentinel. Table-driven over the status matrix.
- **Structural invariants:** self-approval at `Quorum: 1` and `Quorum: 2`; double-vote from one approver in both directions; reject-after-approve; approve-after-reject.
- **Concurrency:** N goroutines approving the same request with `Quorum: 2` — exactly two decisions recorded, status `Approved` exactly once, no torn state. Same for `Claim`: N executors, exactly one winner. Run under `-race`.
- **CAS retry:** a store wrapper that injects `ErrConflict` on the first K attempts proves the retry loop re-validates rather than blindly reapplying — specifically, a vote that lost a race must not be double-recorded.
- **Expiry:** lazy status on `Get`/`List`; every transition on an expired request; `Approved` expiring before claim; `Executing` *not* expiring under `TTL`.
- **Claim lease:** stale re-claim with `ClaimTTL > 0`; wedge (no re-claim) with `ClaimTTL == 0`; `Complete`/`Fail` from a non-holder → `ErrNotClaimHolder`; `Release` from a non-holder *succeeds* (the escape hatch) and returns the request to `Approved` for re-claim.
- **Eligibility:** allow, deny, and decider-error (fail closed); action string and `Attrs` contents asserted via a recording decider; denied attempt audited.
- **Audit:** event per transition asserted against a memory sink; `ErrAuditFailed` returned with a durable transition when the sink fails.
- **Tenancy:** cross-tenant `Get`/transition → `ErrNotFound`; `List` confinement; hook error and empty tenant → `ErrScope`; explicit-tenant disagreement rejected.
- **Store contract:** a shared conformance suite run against `memory` and (integration-tagged) `pgstore`, so both prove the same `ErrConflict`/`ErrNotFound`/`ErrDuplicate` semantics.
- **pgstore:** `//go:build integration`, testcontainers via `testkit/pgtest`, per-package container. Includes a real concurrent-vote test through Postgres.
- **Fuzz:** `PayloadOf` against arbitrary stored bytes (must error, never panic).

## Performance

Benchmarks required in `bench_test.go`, with a post-benchmark optimization pass and before/after numbers in the PR:

- `BenchmarkSubmit` — payload marshal + create.
- `BenchmarkApprove` — the vote path at `Quorum: 2` and at `Quorum: 5`, uncontended.
- `BenchmarkApproveContended` — parallel voters through the CAS retry loop.
- `BenchmarkGet` — read + lazy expiry evaluation (the hot read; the approvals inbox hits it per row).
- `BenchmarkList` — 100 records with status post-filtering.
- `BenchmarkPayloadOf` — typed decode.

Allocation notes carried into implementation: preallocate `Decisions` to `Quorum` capacity on submit; do not re-marshal the payload on any read path (it is stored and returned as raw bytes); the kind registry is a plain map read without a lock because it is immutable after `New`; `applyExpiry` is a pure timestamp comparison with no allocation.

## Documentation

`doc.go` carries a runnable example of the full payout flow (submit → two approvals → claim → complete) plus the multi-tenant and relational-eligibility variants shown during design. `docs/packages.md` loses its `ops/approval` entry on merge — the catalog lists only unbuilt packages.
