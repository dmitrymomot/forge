# ops/approval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `ops/approval` — maker-checker dual control: typed approval requests a second person approves or rejects over a storage-agnostic Store, with an execute-once claim so an approved action runs exactly once.

**Architecture:** A non-generic `Manager` owns a kind→`Policy` registry (immutable after `New`) and drives every state transition through one concurrency primitive: whole-record compare-and-swap on `Store.Update(ctx, r, expectVersion)`. Type safety lives in package-level generic functions (`Submit`, `PayloadOf`, `WithKind`) over a `Kind[T]` token, mirroring `async/queue`. Expiry is derived on read, never written. Eligibility rides `auth/access`; the trail rides `ops/auditlog`; both are optional seams.

**Tech Stack:** Go 1.26, stdlib + `github.com/dmitrymomot/forge/{core/id,core/clock,auth/access,ops/auditlog}`; `pgx/v5` isolated in `ops/approval/pgstore`; tests use `testify` and `testkit/pgtest`.

**Spec:** [2026-07-21-ops-approval-design.md](../specs/2026-07-21-ops-approval-design.md) — read it before Task 1.

## Global Constraints

- **Module path:** `github.com/dmitrymomot/forge`. Package lives at `ops/approval`, import path `github.com/dmitrymomot/forge/ops/approval`.
- **Tests are black-box:** every test file is `package approval_test`. No white-box test files in this package.
- **Run `just fmt ./ops/approval/...` after every file change.** Never `just fmt` a single file — it trips betteralign.
- **Run `just lint` before the final commit.** It runs vet, build, golangci-lint, nilaway, betteralign, modernize, and the integration-tagged pass.
- **`just test ./ops/approval/...`** runs unit tests with `-race`. Integration tier is `just test-integration ./ops/approval/...`.
- **Field order in structs is betteralign's call.** Write fields in readable order; `just fmt` reorders them. Do not fight it.
- **All timestamps:** `.UTC().Truncate(time.Microsecond)` before they enter a `Request`. Postgres `timestamptz` is microsecond-precision; untruncated nanoseconds do not survive the round-trip and break equality assertions.
- **No `time.Now()` in package code.** Always `m.cfg.clock.Now()`, so tests are deterministic.
- **Errors are single-line sentinels** in `errors.go`, prefixed `approval: `, matched with `errors.Is`.
- **Go 1.26:** use `new(expr)` where a pointer to a value is needed; run `go tool modernize` (part of `just lint`). Note: the `claude-review` bot false-flags `new(expr)` as invalid — it is valid Go 1.26; reject that review comment with a link to the spec of `new`.
- **Commit after every task** with a `feat(approval):` / `test(approval):` conventional prefix. No Claude attribution in any commit message.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `ops/approval/errors.go` | All sentinels |
| `ops/approval/approval.go` | `Status`, `Vote`, `Request`, `Decision`, `Kind[T]`, `Actor`, `SubmitParams`, `Filter` |
| `ops/approval/policy.go` | `Policy` + validation |
| `ops/approval/store.go` | `Store` interface |
| `ops/approval/memory.go` | In-memory `Store` |
| `ops/approval/options.go` | `config`, `Option`, all `With*` |
| `ops/approval/manager.go` | `New`, `Get`, `List`, `applyExpiry`, `mutate` CAS loop, scope resolution |
| `ops/approval/submit.go` | `Submit[T]`, `PayloadOf[T]` |
| `ops/approval/decide.go` | `Approve`, `Reject`, `Cancel` |
| `ops/approval/execute.go` | `Claim`, `Complete`, `Fail`, `Release`, `Execute` |
| `ops/approval/eligibility.go` | Decider invocation, fail-closed mapping |
| `ops/approval/audit.go` | auditlog emission |
| `ops/approval/doc.go` | Package doc + runnable example |
| `ops/approval/approvaltest/` | Exported Store conformance suite (`Run`), shared by memory and pgstore |
| `ops/approval/pgstore/` | Postgres driver, migrations, integration tests |

---

## Task 1: Core types and errors

**Files:**
- Create: `ops/approval/errors.go`
- Create: `ops/approval/approval.go`
- Test: `ops/approval/approval_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Status` (+ `String`, `Terminal`), `Vote`, `Request`, `Decision`, `Kind[T]` (+ `NewKind[T]`, `Name`), `Actor`, `SubmitParams`, `Filter`, and every sentinel in `errors.go`. Every later task depends on these names.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/approval_test.go`:

```go
package approval_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/approval"
)

func TestStatusString(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		want string
		s    approval.Status
	}{
		{"pending", approval.Pending},
		{"approved", approval.Approved},
		{"rejected", approval.Rejected},
		{"cancelled", approval.Cancelled},
		{"expired", approval.Expired},
		{"executing", approval.Executing},
		{"executed", approval.Executed},
		{"failed", approval.Failed},
		{"unknown", approval.Status(99)},
	} {
		assert.Equal(t, tc.want, tc.s.String())
	}
}

func TestStatusTerminal(t *testing.T) {
	t.Parallel()
	terminal := []approval.Status{
		approval.Rejected, approval.Cancelled, approval.Expired,
		approval.Executed, approval.Failed,
	}
	for _, s := range terminal {
		assert.True(t, s.Terminal(), "%s must be terminal", s)
	}
	nonTerminal := []approval.Status{approval.Pending, approval.Approved, approval.Executing}
	for _, s := range nonTerminal {
		assert.False(t, s.Terminal(), "%s must not be terminal", s)
	}
}

func TestNewKind(t *testing.T) {
	t.Parallel()
	k := approval.NewKind[struct{ A int }]("payout.release")
	require.Equal(t, "payout.release", k.Name())
	assert.Panics(t, func() { approval.NewKind[int]("") }, "empty name is a wiring bug")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `no required module provides package github.com/dmitrymomot/forge/ops/approval`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/errors.go`:

```go
package approval

import "errors"

var (
	// ErrNotFound is returned by a Store when no request matches, and by
	// Manager operations for other tenants' requests under WithScope (so
	// cross-tenant existence cannot be probed).
	ErrNotFound = errors.New("approval: request not found")

	// ErrDuplicate is returned by Store.Create when the ID already exists.
	ErrDuplicate = errors.New("approval: duplicate request")

	// ErrConflict is returned by Store.Update when the stored Version no
	// longer matches the expected one. The Manager retries on it.
	ErrConflict = errors.New("approval: version conflict")

	// ErrUnknownKind rejects a submission for a kind that was not
	// registered with WithKind.
	ErrUnknownKind = errors.New("approval: unknown kind")

	// ErrKindMismatch rejects PayloadOf when the Kind does not match the
	// request's kind — the payload would decode into the wrong type.
	ErrKindMismatch = errors.New("approval: kind mismatch")

	// ErrRequesterRequired rejects SubmitParams with an empty Requester.
	ErrRequesterRequired = errors.New("approval: requester required")

	// ErrActorRequired rejects a decision from an Actor with an empty
	// Subject.ID — an anonymous checker cannot satisfy dual control.
	ErrActorRequired = errors.New("approval: actor required")

	// ErrExecutorRequired rejects a claim with an empty executor id.
	ErrExecutorRequired = errors.New("approval: executor required")

	// ErrSelfApproval enforces the maker-checker rule: the requester may
	// never decide their own request, at any quorum.
	ErrSelfApproval = errors.New("approval: requester cannot decide own request")

	// ErrAlreadyVoted rejects a second decision from an approver who has
	// already voted, whichever way they voted first.
	ErrAlreadyVoted = errors.New("approval: approver already voted")

	// ErrNotPending rejects a decision on a request that is no longer
	// awaiting one.
	ErrNotPending = errors.New("approval: request not pending")

	// ErrExpired rejects every transition on a request past its TTL.
	ErrExpired = errors.New("approval: request expired")

	// ErrNotApproved rejects a claim on a request that has not reached
	// quorum.
	ErrNotApproved = errors.New("approval: request not approved")

	// ErrAlreadyClaimed rejects a claim held by another executor whose
	// lease has not gone stale.
	ErrAlreadyClaimed = errors.New("approval: request already claimed")

	// ErrNotExecuting rejects Complete, Fail, or Release on a request that
	// is not currently claimed.
	ErrNotExecuting = errors.New("approval: request not executing")

	// ErrNotClaimHolder rejects Complete or Fail from an executor other
	// than the one holding the claim. Release is deliberately exempt.
	ErrNotClaimHolder = errors.New("approval: executor does not hold the claim")

	// ErrNotCancellable rejects Cancel on a request that is executing or
	// already terminal.
	ErrNotCancellable = errors.New("approval: request not cancellable")

	// ErrNotEligible rejects a decision from an actor the decider denied,
	// or whose eligibility could not be established (fail closed).
	ErrNotEligible = errors.New("approval: actor not eligible")

	// ErrScope is the fail-closed result of a WithScope hook that errored
	// or returned an empty tenant, or of a request tenant that disagrees
	// with the scoped one.
	ErrScope = errors.New("approval: tenant scope unavailable")

	// ErrAuditFailed reports that a transition was persisted but its audit
	// event could not be written. The returned Request is durable; the
	// trail is not. Match it with errors.Is and alert — never swallow it.
	ErrAuditFailed = errors.New("approval: audit write failed")
)
```

Create `ops/approval/approval.go`:

```go
package approval

import (
	"encoding/json"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/id"
)

// Status is the lifecycle state of an approval request.
type Status uint8

// Request statuses. Expired is derived on read from ExpiresAt and is never
// persisted — see Manager.Get.
const (
	Pending   Status = iota // awaiting decisions
	Approved                // quorum met, awaiting claim
	Rejected                // a checker rejected it
	Cancelled               // withdrawn before execution
	Expired                 // TTL elapsed while Pending or Approved
	Executing               // claimed by an executor
	Executed                // executor reported success
	Failed                  // executor reported failure
)

// String renders the status for logs, audit metadata, and errors.
func (s Status) String() string {
	switch s {
	case Pending:
		return "pending"
	case Approved:
		return "approved"
	case Rejected:
		return "rejected"
	case Cancelled:
		return "cancelled"
	case Expired:
		return "expired"
	case Executing:
		return "executing"
	case Executed:
		return "executed"
	case Failed:
		return "failed"
	default:
		return "unknown"
	}
}

// Terminal reports whether the status admits no further transitions.
func (s Status) Terminal() bool {
	switch s {
	case Rejected, Cancelled, Expired, Executed, Failed:
		return true
	default:
		return false
	}
}

// Vote is the direction of a checker's decision.
type Vote uint8

// Vote directions. The zero value is invalid so an unset Vote cannot pass
// for an approval.
const (
	VoteApprove Vote = iota + 1
	VoteReject
)

// String renders the vote for audit metadata.
func (v Vote) String() string {
	switch v {
	case VoteApprove:
		return "approve"
	case VoteReject:
		return "reject"
	default:
		return "unknown"
	}
}

// Kind binds an approval action name to its payload type T. Declare one
// package-level Kind per action and share it between the code that submits
// and the code that executes: the name exists in exactly one place, and
// payload type drift becomes a compile error.
//
//	var KindReleasePayout = approval.NewKind[ReleasePayout]("payout.release")
type Kind[T any] struct {
	name string
}

// NewKind creates a Kind for payload type T. Panics on an empty name:
// kinds are package-level wiring, not runtime data.
func NewKind[T any](name string) Kind[T] {
	if name == "" {
		panic("approval: NewKind requires a non-empty name")
	}
	return Kind[T]{name: name}
}

// Name returns the action name.
func (k Kind[T]) Name() string { return k.name }

// Decision is one checker's vote on a request. Decisions are append-only.
type Decision struct {
	// At is when the decision was cast, UTC, microsecond precision.
	At time.Time `json:"at"`
	// Approver is the deciding subject's id.
	Approver string `json:"approver"`
	// Reason is the checker's free-form justification.
	Reason string `json:"reason,omitempty"`
	// Vote is the direction of the decision.
	Vote Vote `json:"vote"`
}

// Request is one privileged action awaiting dual control.
type Request struct {
	// CreatedAt is when the request was submitted.
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is when the request stops being actionable. Zero means it
	// never expires.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// ClaimedAt is when the current executor claimed it. Zero when
	// unclaimed.
	ClaimedAt time.Time `json:"claimed_at,omitzero"`
	// DecidedAt is when the request reached Approved or a terminal status.
	DecidedAt time.Time `json:"decided_at,omitzero"`
	// Decisions holds every vote cast, in the order cast.
	Decisions []Decision `json:"decisions,omitempty"`
	// Meta carries free-form submitter context.
	Meta map[string]string `json:"meta,omitempty"`
	// Payload is the JSON-encoded action payload. Decode it with PayloadOf.
	Payload json.RawMessage `json:"payload"`
	// Kind is the registered action name.
	Kind string `json:"kind"`
	// Tenant is the owning tenant; empty in single-tenant applications.
	Tenant string `json:"tenant,omitempty"`
	// Requester is the maker — the principal that submitted the request.
	Requester string `json:"requester"`
	// Reason is the maker's justification, shown to checkers.
	Reason string `json:"reason,omitempty"`
	// ClaimedBy is the executor currently holding the claim.
	ClaimedBy string `json:"claimed_by,omitempty"`
	// ID is a time-ordered UUIDv7 assigned at submit.
	ID id.UUID `json:"id"`
	// Version is the optimistic-concurrency counter. Store.Update accepts a
	// write only when the stored value matches.
	Version int64 `json:"version"`
	// Status is the lifecycle state.
	Status Status `json:"status"`
}

// Approvals counts the approving votes cast so far.
func (r Request) Approvals() int {
	n := 0
	for i := range r.Decisions {
		if r.Decisions[i].Vote == VoteApprove {
			n++
		}
	}
	return n
}

// SubmitParams carries the maker's side of a submission.
type SubmitParams struct {
	// Meta is free-form context, cloned on submit.
	Meta map[string]string
	// Requester is the maker's principal id. Required.
	Requester string
	// Tenant is optional; under WithScope it must be empty or equal to the
	// scoped tenant.
	Tenant string
	// Reason is the maker's justification, persisted on the request.
	Reason string
}

// Actor is the human acting on a request — a checker casting a decision, or
// the party cancelling. It carries an access.Subject rather than a bare id
// so a decider has real attributes to judge on. Executors are machines and
// pass a plain executor id instead.
type Actor struct {
	Subject access.Subject
	Reason  string
}

// Filter selects requests for List.
type Filter struct {
	// Statuses matches the STORED status. Expired is never stored (it is
	// derived on read), so listing it matches nothing — query
	// []Status{Pending, Approved} with ExpiresBefore instead.
	Statuses []Status
	// ExpiresBefore bounds ExpiresAt, selecting requests that have expired
	// or are about to. Zero means no bound. Requests with no expiry are
	// never matched by it.
	ExpiresBefore time.Time
	// Kind, Tenant, and Requester are exact matches; empty means "any".
	Kind      string
	Tenant    string
	Requester string
	// Limit caps the number of records returned. Zero means the store's
	// default.
	Limit int
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS — `ok github.com/dmitrymomot/forge/ops/approval`.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): core request, status, vote, and kind types"
```

---

## Task 2: Store interface, in-memory store, and the conformance suite

**Files:**
- Create: `ops/approval/store.go`
- Create: `ops/approval/memory.go`
- Create: `ops/approval/approvaltest/approvaltest.go`
- Test: `ops/approval/store_test.go`

**Interfaces:**
- Consumes: `Request`, `Filter`, `Status`, `ErrNotFound`, `ErrDuplicate`, `ErrConflict` from Task 1.
- Produces: `Store` interface (`Create`, `Get`, `List`, `Update`), `NewMemoryStore() Store`, and `approvaltest.Run(t *testing.T, factory func(t *testing.T) approval.Store)` — the shared conformance suite `pgstore` reruns in Task 12.

The suite lives in its own package, mirroring `async/queue/brokertest`: an
integration-tagged `pgstore_test` cannot import `package approval_test`, so
the contract has to be importable production code.

**Critical:** the Postgres table persists across test runs (`pgstore_test`
does not truncate, matching `auth/apikey/pgstore/pgstore_test.go`). Every
subtest therefore namespaces its fixtures with a fresh UUID and filters
`List` by that namespace. A suite with fixed kinds and exact global counts
passes once and fails on every re-run.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/approvaltest/approvaltest.go`:

```go
// Package approvaltest is the executable contract for approval.Store
// implementations. Every driver's test suite must call Run; the in-memory
// store is the reference implementation.
//
// Fixtures are namespaced per subtest with a fresh UUID because a real
// backend's table outlives the test process: deterministic kinds and
// tenants would collide with a previous run's rows and inflate List counts.
package approvaltest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

// ns is a per-subtest namespace keeping fixtures disjoint from every other
// run against the same backend.
type ns struct{ kind, tenant string }

func newNS() ns {
	u := id.NewUUID().String()
	return ns{kind: "kind-" + u, tenant: "tenant-" + u}
}

func (n ns) request(requester string, status approval.Status) approval.Request {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return approval.Request{
		ID:        id.NewUUID(),
		Kind:      n.kind,
		Tenant:    n.tenant,
		Requester: requester,
		Status:    status,
		Version:   1,
		Payload:   json.RawMessage(`{"amount":100}`),
		CreatedAt: now,
	}
}

// Run executes the Store conformance suite. factory must return a fresh or
// namespace-isolated store each call. The Manager's CAS retry loop depends
// on these exact semantics, so an implementation that diverges here breaks
// dual control.
func Run(t *testing.T, factory func(t *testing.T) approval.Store) {
	t.Helper()

	t.Run("CreateGetRoundTrip", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))

		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		assert.Equal(t, r.ID, got.ID)
		assert.Equal(t, r.Kind, got.Kind)
		assert.Equal(t, r.Requester, got.Requester)
		assert.Equal(t, approval.Pending, got.Status)
		assert.Equal(t, int64(1), got.Version)
		assert.JSONEq(t, string(r.Payload), string(got.Payload))
		assert.True(t, r.CreatedAt.Equal(got.CreatedAt), "timestamps survive the round-trip")
	})

	t.Run("CreateRejectsDuplicateID", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))
		assert.ErrorIs(t, s.Create(ctx, r), approval.ErrDuplicate)
	})

	t.Run("GetReportsNotFound", func(t *testing.T) {
		s := factory(t)
		_, err := s.Get(context.Background(), id.NewUUID())
		assert.ErrorIs(t, err, approval.ErrNotFound)
	})

	t.Run("UpdateAdvancesVersion", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))

		r.Status = approval.Approved
		require.NoError(t, s.Update(ctx, r, 1))

		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		assert.Equal(t, approval.Approved, got.Status)
		assert.Equal(t, int64(2), got.Version, "store sets version to expect+1")
	})

	t.Run("UpdateConflictsOnStaleVersion", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		require.NoError(t, s.Create(ctx, r))
		require.NoError(t, s.Update(ctx, r, 1))

		// A second writer still believing the version is 1 must lose.
		assert.ErrorIs(t, s.Update(ctx, r, 1), approval.ErrConflict)
	})

	t.Run("UpdateReportsNotFound", func(t *testing.T) {
		s, n := factory(t), newNS()
		assert.ErrorIs(t, s.Update(context.Background(), n.request("alice", approval.Pending), 1),
			approval.ErrNotFound)
	})

	t.Run("StoredStateIsNotAliased", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		r := n.request("alice", approval.Pending)
		r.Decisions = []approval.Decision{{
			At: time.Now().UTC().Truncate(time.Microsecond), Approver: "bob", Vote: approval.VoteApprove,
		}}
		require.NoError(t, s.Create(ctx, r))

		r.Decisions[0].Approver = "mallory" // mutate the caller's slice
		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		require.Len(t, got.Decisions, 1)
		assert.Equal(t, "bob", got.Decisions[0].Approver,
			"a caller must not be able to rewrite a persisted vote")
	})

	t.Run("DecisionsRoundTrip", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		at := time.Now().UTC().Truncate(time.Microsecond)
		r := n.request("alice", approval.Pending)
		r.Decisions = []approval.Decision{
			{At: at, Approver: "bob", Reason: "checked", Vote: approval.VoteApprove},
			{At: at, Approver: "carol", Vote: approval.VoteReject},
		}
		require.NoError(t, s.Create(ctx, r))

		got, err := s.Get(ctx, r.ID)
		require.NoError(t, err)
		require.Len(t, got.Decisions, 2)
		assert.Equal(t, "bob", got.Decisions[0].Approver)
		assert.Equal(t, "checked", got.Decisions[0].Reason)
		assert.Equal(t, approval.VoteApprove, got.Decisions[0].Vote)
		assert.Equal(t, approval.VoteReject, got.Decisions[1].Vote)
		assert.True(t, at.Equal(got.Decisions[0].At))
	})

	t.Run("ListFilters", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		pending := n.request("alice", approval.Pending)
		approved := n.request("alice", approval.Approved)
		otherRequester := n.request("carol", approval.Pending)
		for _, r := range []approval.Request{pending, approved, otherRequester} {
			require.NoError(t, s.Create(ctx, r))
		}
		// A same-kind row in a different tenant must never leak in.
		foreign := n.request("alice", approval.Pending)
		foreign.Tenant = "tenant-" + id.NewUUID().String()
		require.NoError(t, s.Create(ctx, foreign))

		got, err := s.List(ctx, approval.Filter{
			Statuses:  []approval.Status{approval.Pending},
			Kind:      n.kind,
			Tenant:    n.tenant,
			Requester: "alice",
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, pending.ID, got[0].ID)
	})

	t.Run("ListFiltersByExpiryBound", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		now := time.Now().UTC().Truncate(time.Microsecond)

		soon := n.request("alice", approval.Pending)
		soon.ExpiresAt = now.Add(time.Minute)
		late := n.request("alice", approval.Pending)
		late.ExpiresAt = now.Add(time.Hour)
		never := n.request("alice", approval.Pending)
		for _, r := range []approval.Request{soon, late, never} {
			require.NoError(t, s.Create(ctx, r))
		}

		got, err := s.List(ctx, approval.Filter{
			Tenant:        n.tenant,
			ExpiresBefore: now.Add(10 * time.Minute),
		})
		require.NoError(t, err)
		require.Len(t, got, 1, "never-expiring rows are excluded from an expiry bound")
		assert.Equal(t, soon.ID, got[0].ID)
	})

	t.Run("ListLimitAndOrder", func(t *testing.T) {
		s, n, ctx := factory(t), newNS(), context.Background()
		var ids []id.UUID
		for range 5 {
			r := n.request("alice", approval.Pending)
			require.NoError(t, s.Create(ctx, r))
			ids = append(ids, r.ID)
			time.Sleep(2 * time.Millisecond) // UUIDv7 order is millisecond-grained
		}

		got, err := s.List(ctx, approval.Filter{Tenant: n.tenant, Limit: 2})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, ids[4], got[0].ID, "newest first")
		assert.Equal(t, ids[3], got[1].ID)
	})

	t.Run("ListReturnsNonNilEmpty", func(t *testing.T) {
		s := factory(t)
		got, err := s.List(context.Background(), approval.Filter{Kind: "kind-" + id.NewUUID().String()})
		require.NoError(t, err)
		assert.NotNil(t, got, "nil vs empty must not differ across implementations")
		assert.Empty(t, got)
	})
}
```

Create `ops/approval/store_test.go`:

```go
package approval_test

import (
	"testing"

	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/approval/approvaltest"
)

func TestMemoryStoreContract(t *testing.T) {
	t.Parallel()
	approvaltest.Run(t, func(t *testing.T) approval.Store {
		return approval.NewMemoryStore()
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `undefined: approval.Store`, `undefined: approval.NewMemoryStore`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/store.go`:

```go
package approval

import (
	"context"

	"github.com/dmitrymomot/forge/core/id"
)

// Store persists approval requests. Implementations must be safe for
// concurrent use.
//
// Update is the package's only concurrency primitive: every state
// transition is a compare-and-swap on Version. An implementation that does
// not enforce it atomically breaks dual control — two checkers voting
// concurrently could both read quorum-1 approvals and both write, losing a
// vote or counting one approver twice.
//
// Implementations may normalize nil and empty Decisions/Meta in either
// direction; callers must not depend on which form is returned. List must
// return a non-nil empty slice rather than nil when nothing matches.
//
// approvaltest.Run is the executable contract; every implementation must
// pass it.
type Store interface {
	// Create persists a new request. It returns ErrDuplicate when a
	// request with the same ID already exists.
	Create(ctx context.Context, r Request) error

	// Get loads one request. It returns ErrNotFound for unknown ids.
	Get(ctx context.Context, reqID id.UUID) (Request, error)

	// List returns requests matching f, newest first (UUIDv7 id order;
	// ties within one millisecond are unordered).
	List(ctx context.Context, f Filter) ([]Request, error)

	// Update persists r only when the stored Version equals expect,
	// returning ErrConflict otherwise and ErrNotFound for unknown ids. The
	// implementation persists r with Version set to expect+1.
	Update(ctx context.Context, r Request, expect int64) error
}
```

Create `ops/approval/memory.go`:

```go
package approval

import (
	"context"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/dmitrymomot/forge/core/id"
)

type memoryStore struct {
	byID map[id.UUID]Request
	mu   sync.RWMutex
}

// NewMemoryStore returns an in-memory Store for tests and development. It
// is not durable: process exit loses every request.
func NewMemoryStore() Store {
	return &memoryStore{byID: make(map[id.UUID]Request)}
}

// cloneRequest copies the reference fields so callers cannot mutate stored
// state through a shared slice or map, and vice versa. Aliased Decisions
// would be a dual-control hole: a caller could rewrite a persisted vote.
func cloneRequest(r Request) Request {
	r.Decisions = slices.Clone(r.Decisions)
	r.Meta = maps.Clone(r.Meta)
	r.Payload = slices.Clone(r.Payload)
	return r
}

func (s *memoryStore) Create(_ context.Context, r Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[r.ID]; ok {
		return ErrDuplicate
	}
	s.byID[r.ID] = cloneRequest(r)
	return nil
}

func (s *memoryStore) Get(_ context.Context, reqID id.UUID) (Request, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byID[reqID]
	if !ok {
		return Request{}, ErrNotFound
	}
	return cloneRequest(r), nil
}

func (s *memoryStore) Update(_ context.Context, r Request, expect int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.byID[r.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expect {
		return ErrConflict
	}
	next := cloneRequest(r)
	next.Version = expect + 1
	s.byID[r.ID] = next
	return nil
}

func (s *memoryStore) List(_ context.Context, f Filter) ([]Request, error) {
	s.mu.RLock()
	out := make([]Request, 0, len(s.byID))
	for _, r := range s.byID {
		if !matches(r, f) {
			continue
		}
		out = append(out, cloneRequest(r))
	}
	s.mu.RUnlock()

	// UUIDv7 ids are time-ordered, so descending id order is newest-first.
	sort.Slice(out, func(i, j int) bool {
		return string(out[i].ID[:]) > string(out[j].ID[:])
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func matches(r Request, f Filter) bool {
	if f.Kind != "" && r.Kind != f.Kind {
		return false
	}
	if f.Tenant != "" && r.Tenant != f.Tenant {
		return false
	}
	if f.Requester != "" && r.Requester != f.Requester {
		return false
	}
	if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, r.Status) {
		return false
	}
	if !f.ExpiresBefore.IsZero() {
		// Rows with no expiry never match an expiry bound.
		if r.ExpiresAt.IsZero() || !r.ExpiresAt.Before(f.ExpiresBefore) {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS — every `TestMemoryStoreContract` subtest green.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): store seam, conformance suite, and in-memory implementation"
```

---

## Task 3: Policy, options, and New

**Files:**
- Create: `ops/approval/policy.go`
- Create: `ops/approval/options.go`
- Create: `ops/approval/manager.go`
- Test: `ops/approval/manager_test.go`

**Interfaces:**
- Consumes: `Store`, `Kind[T]`, sentinels from Tasks 1–2.
- Produces: `Policy{Quorum, TTL, ClaimTTL}`, `Manager`, `New(store Store, opts ...Option) *Manager`, `Option`, `WithKind[T](k Kind[T], p Policy) Option`, `WithClock(clock.Clock) Option`, `WithMaxRetries(int) Option`, and the unexported `(*Manager).policyFor(kind string) (Policy, bool)` used by Tasks 4–8. `WithDecider`, `WithAuditor`, and `WithScope` are added in Tasks 9–11.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/manager_test.go`:

```go
package approval_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/approval"
)

type payoutPayload struct {
	PayoutID string `json:"payout_id"`
	Amount   int64  `json:"amount"`
}

var kindPayout = approval.NewKind[payoutPayload]("payout.release")

func TestNewPanics(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "approval: nil store", func() {
		approval.New(nil, approval.WithKind(kindPayout, approval.Policy{Quorum: 2}))
	})

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 0}))
	}, "quorum below 1 is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 3}))
	}, "duplicate kind registration is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: -time.Hour}))
	}, "negative TTL is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore(),
			approval.WithKind(kindPayout, approval.Policy{Quorum: 2, ClaimTTL: -time.Hour}))
	}, "negative ClaimTTL is a wiring bug")

	assert.Panics(t, func() {
		approval.New(approval.NewMemoryStore())
	}, "a manager with no registered kinds can never accept a submission")
}

func TestNewAcceptsValidWiring(t *testing.T) {
	t.Parallel()
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: 24 * time.Hour}))
	require.NotNil(t, m)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `undefined: approval.New`, `undefined: approval.Policy`, `undefined: approval.WithKind`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/policy.go`:

```go
package approval

import (
	"fmt"
	"time"
)

// Policy is the per-kind rule set. It is registered at construction with
// WithKind and never supplied by the caller at submit time: a caller who
// could choose the quorum could weaken the gate on the action they are
// asking permission for.
type Policy struct {
	// TTL is how long a request stays actionable after submission. Zero
	// means it never expires.
	TTL time.Duration
	// ClaimTTL is how long an executor's claim is held before another
	// executor may take it over. Zero — the default — means the claim never
	// expires: an executor that dies mid-action wedges the request until
	// Release is called. That is the safe default for actions that move
	// money. Setting it non-zero opts into at-least-once execution, so the
	// action behind it must be idempotent.
	ClaimTTL time.Duration
	// Quorum is the number of distinct approvals required. Must be at
	// least 1. Note that Quorum 1 is still dual control: the requester can
	// never be the approver.
	Quorum int
}

// validate reports why a policy is unusable, or nil.
func (p Policy) validate(kind string) error {
	if p.Quorum < 1 {
		return fmt.Errorf("approval: kind %q: quorum must be >= 1, got %d", kind, p.Quorum)
	}
	if p.TTL < 0 {
		return fmt.Errorf("approval: kind %q: negative TTL %s", kind, p.TTL)
	}
	if p.ClaimTTL < 0 {
		return fmt.Errorf("approval: kind %q: negative ClaimTTL %s", kind, p.ClaimTTL)
	}
	return nil
}
```

Create `ops/approval/options.go`:

```go
package approval

import (
	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	clk        clock.Clock
	kinds      map[string]Policy
	maxRetries int
}

// Option configures New.
type Option func(*config)

// WithKind registers an action name and the policy that governs it. It is
// the only way a kind enters a Manager — the registry is immutable after
// New, so the read path needs no lock and no caller can register a weaker
// policy at runtime. Repeat it once per action.
//
// New panics on a duplicate name, a quorum below 1, or a negative duration.
func WithKind[T any](k Kind[T], p Policy) Option {
	return func(c *config) {
		name := k.Name()
		if _, dup := c.kinds[name]; dup {
			panic("approval: duplicate kind " + name)
		}
		if err := p.validate(name); err != nil {
			panic(err.Error())
		}
		c.kinds[name] = p
	}
}

// WithClock injects a clock for deterministic tests. Defaults to
// clock.System().
func WithClock(clk clock.Clock) Option {
	return func(c *config) {
		if clk != nil {
			c.clk = clk
		}
	}
}

// WithMaxRetries caps how many times a transition re-reads and retries
// after losing a version race (default 3). Each retry re-validates from a
// fresh read, so a retry can still legitimately fail with ErrAlreadyVoted.
// Values below zero are clamped to zero (a single attempt).
func WithMaxRetries(n int) Option {
	return func(c *config) {
		if n < 0 {
			n = 0
		}
		c.maxRetries = n
	}
}
```

Create `ops/approval/manager.go`:

```go
package approval

import (
	"context"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// Manager records approval requests, collects decisions on them, and hands
// approved requests to exactly one executor. Safe for concurrent use.
type Manager struct {
	store Store
	cfg   config
}

// New builds a Manager over store. Register at least one kind with
// WithKind.
//
// It panics on a nil store, on a Manager with no registered kinds, and on
// any invalid policy — wiring bugs caught at startup rather than on the
// first payout, matching apikey.New's nil-store panic.
func New(store Store, opts ...Option) *Manager {
	if store == nil {
		panic("approval: nil store")
	}
	cfg := config{
		clk:        clock.System(),
		kinds:      make(map[string]Policy),
		maxRetries: 3,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if len(cfg.kinds) == 0 {
		panic("approval: no kinds registered; every submission would fail with ErrUnknownKind")
	}
	return &Manager{store: store, cfg: cfg}
}

// policyFor returns the policy registered for kind. The registry is
// immutable after New, so this read needs no lock.
func (m *Manager) policyFor(kind string) (Policy, bool) {
	p, ok := m.cfg.kinds[kind]
	return p, ok
}

// Get loads one request, with expiry applied: a Pending or Approved
// request past its ExpiresAt reports Status Expired even though the stored
// row still carries its last written status.
func (m *Manager) Get(ctx context.Context, reqID id.UUID) (Request, error) {
	r, err := m.store.Get(ctx, reqID)
	if err != nil {
		return Request{}, err
	}
	m.applyExpiry(&r)
	return r, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

`applyExpiry` does not exist yet — add the minimal version now; Task 5 tests it directly. Append to `ops/approval/manager.go`:

```go
// applyExpiry derives the effective status of r. Expiry is never written:
// the stored row keeps its last written status and the effective status is
// computed on every read, so no sweeper is needed for correctness. Only
// Pending and Approved expire — an Executing request is governed by its
// claim lease, not by TTL.
func (m *Manager) applyExpiry(r *Request) {
	if r.ExpiresAt.IsZero() {
		return
	}
	if r.Status != Pending && r.Status != Approved {
		return
	}
	if m.cfg.clk.Now().Before(r.ExpiresAt) {
		return
	}
	r.Status = Expired
}
```

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): policy registry, options, and manager construction"
```

---

## Task 4: Submit and PayloadOf

**Files:**
- Create: `ops/approval/submit.go`
- Test: `ops/approval/submit_test.go`

**Interfaces:**
- Consumes: `Manager`, `policyFor`, `Kind[T]`, `SubmitParams`, `Request` from Tasks 1–3.
- Produces: `Submit[T](ctx, m *Manager, k Kind[T], payload T, p SubmitParams) (Request, error)` and `PayloadOf[T](k Kind[T], r Request) (T, error)`, plus the unexported `(*Manager).now() time.Time` helper used by every later task.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/submit_test.go`:

```go
package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

var fixedNow = time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

// newManager builds a Manager with a fixed clock and the payout kind.
func newManager(t *testing.T, p approval.Policy, extra ...approval.Option) *approval.Manager {
	t.Helper()
	opts := append([]approval.Option{
		approval.WithKind(kindPayout, p),
		approval.WithClock(clock.NewMock(fixedNow)),
	}, extra...)
	return approval.New(approval.NewMemoryStore(), opts...)
}

func TestSubmit(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "invoice #4471"})
	require.NoError(t, err)

	assert.False(t, r.ID.IsZero())
	assert.Equal(t, "payout.release", r.Kind)
	assert.Equal(t, "alice", r.Requester)
	assert.Equal(t, "invoice #4471", r.Reason)
	assert.Equal(t, approval.Pending, r.Status)
	assert.Equal(t, int64(1), r.Version)
	assert.Empty(t, r.Decisions)
	assert.True(t, fixedNow.Equal(r.CreatedAt))
	assert.True(t, fixedNow.Add(24*time.Hour).Equal(r.ExpiresAt))
}

func TestSubmitZeroTTLNeverExpires(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 1})
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_1"}, approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	assert.True(t, r.ExpiresAt.IsZero())
}

func TestSubmitRejectsEmptyRequester(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	_, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{}, approval.SubmitParams{})
	assert.ErrorIs(t, err, approval.ErrRequesterRequired)
}

func TestSubmitRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	unregistered := approval.NewKind[payoutPayload]("payout.unregistered")
	m := newManager(t, approval.Policy{Quorum: 2})
	_, err := approval.Submit(context.Background(), m, unregistered,
		payoutPayload{}, approval.SubmitParams{Requester: "alice"})
	assert.ErrorIs(t, err, approval.ErrUnknownKind)
}

func TestSubmitClonesMeta(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	meta := map[string]string{"request_id": "req_1"}

	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{}, approval.SubmitParams{Requester: "alice", Meta: meta})
	require.NoError(t, err)

	meta["request_id"] = "tampered"
	assert.Equal(t, "req_1", r.Meta["request_id"], "meta is cloned at submit")
}

func TestPayloadOf(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	want := payoutPayload{PayoutID: "po_88", Amount: 250000}

	r, err := approval.Submit(context.Background(), m, kindPayout, want,
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	got, err := approval.PayloadOf(kindPayout, r)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestPayloadOfRejectsWrongKind(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_88"}, approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	other := approval.NewKind[struct{ Days int }]("hr.vacation")
	_, err = approval.PayloadOf(other, r)
	assert.ErrorIs(t, err, approval.ErrKindMismatch)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `undefined: approval.Submit`, `undefined: approval.PayloadOf`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/submit.go`:

```go
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// now returns the manager's clock time, normalized to what a Postgres
// timestamptz can hold. Untruncated nanoseconds do not survive the
// round-trip, so a stored request would not equal the one just returned.
func (m *Manager) now() time.Time {
	return m.cfg.clk.Now().UTC().Truncate(time.Microsecond)
}

// Submit records a new approval request for kind k carrying payload.
//
// The returned request is Pending: it becomes actionable to checkers
// immediately and expires after the kind's policy TTL. Submitting does not
// authorize anything by itself — the maker's own decision never counts.
func Submit[T any](ctx context.Context, m *Manager, k Kind[T], payload T, p SubmitParams) (Request, error) {
	pol, ok := m.policyFor(k.Name())
	if !ok {
		return Request{}, ErrUnknownKind
	}
	if p.Requester == "" {
		return Request{}, ErrRequesterRequired
	}
	tenant, err := m.scoped(ctx, p.Tenant)
	if err != nil {
		return Request{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Request{}, fmt.Errorf("approval: marshal payload: %w", err)
	}

	now := m.now()
	r := Request{
		ID:        id.NewUUID(),
		Kind:      k.Name(),
		Tenant:    tenant,
		Requester: p.Requester,
		Reason:    p.Reason,
		Status:    Pending,
		Version:   1,
		Payload:   raw,
		Meta:      maps.Clone(p.Meta),
		Decisions: make([]Decision, 0, pol.Quorum),
		CreatedAt: now,
	}
	if pol.TTL > 0 {
		r.ExpiresAt = now.Add(pol.TTL)
	}
	if err := m.store.Create(ctx, r); err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionSubmit, p.Requester, outcomeSuccess, p.Reason)
}

// PayloadOf decodes r's payload as T. It returns ErrKindMismatch when k is
// not the kind r was submitted under — decoding another action's payload
// into T would silently produce a zero-valued struct.
func PayloadOf[T any](k Kind[T], r Request) (T, error) {
	var out T
	if r.Kind != k.Name() {
		return out, fmt.Errorf("%w: request is %q, kind is %q", ErrKindMismatch, r.Kind, k.Name())
	}
	if err := json.Unmarshal(r.Payload, &out); err != nil {
		return out, fmt.Errorf("approval: unmarshal payload: %w", err)
	}
	return out, nil
}
```

`m.scoped` and `m.audit` do not exist yet (Tasks 10–11). Add temporary no-op versions to `ops/approval/manager.go` so this compiles; Tasks 10 and 11 replace their bodies:

```go
// scoped resolves the tenant an operation is confined to. Tenancy lands in
// Task 11; until then it passes the requested tenant through.
func (m *Manager) scoped(_ context.Context, requested string) (string, error) {
	return requested, nil
}

// audit records a state change. The auditlog seam lands in Task 10; until
// then it is a no-op.
func (m *Manager) audit(_ context.Context, _ Request, _, _, _, _ string) error {
	return nil
}

// Audit action names and outcomes.
const (
	actionSubmit   = "approval.submit"
	outcomeSuccess = "success"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS — all `TestSubmit*` and `TestPayloadOf*` green.

- [ ] **Step 5: Add the fuzz test**

`PayloadOf` decodes bytes that came out of a database, so it must reject
garbage rather than panic — a corrupted or hand-edited payload column must
not take down the approvals UI. Create `ops/approval/fuzz_test.go`:

```go
package approval_test

import (
	"encoding/json"
	"testing"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

func FuzzPayloadOf(f *testing.F) {
	f.Add([]byte(`{"payout_id":"po_1","amount":100}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add([]byte(`{"amount":"not-a-number"}`))
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte("\x00\x01\x02"))

	f.Fuzz(func(t *testing.T, payload []byte) {
		r := approval.Request{
			ID:      id.NewUUID(),
			Kind:    kindPayout.Name(),
			Payload: json.RawMessage(payload),
		}
		// Must never panic; an error is a perfectly good outcome.
		_, _ = approval.PayloadOf(kindPayout, r)
	})
}
```

Run: `go test -run Fuzz -fuzz FuzzPayloadOf -fuzztime=30s ./ops/approval/`
Expected: no crashers. Then `just test ./ops/approval/...` to confirm the
seed corpus passes in the normal run.

- [ ] **Step 6: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): typed submit and payload decode"
```

---

## Task 5: List, lazy expiry, and the CAS retry loop

**Files:**
- Modify: `ops/approval/manager.go`
- Test: `ops/approval/expiry_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–4.
- Produces: `(*Manager).List(ctx, Filter) ([]Request, error)` and the unexported `(*Manager).mutate(ctx, reqID, fn func(*Request) error) (Request, error)` — the single CAS retry loop every transition in Tasks 6–8 is built on.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/expiry_test.go`:

```go
package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

func TestGetAppliesLazyExpiry(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	got, err := m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status)

	clk.Set(fixedNow.Add(time.Hour)) // exactly at ExpiresAt
	got, err = m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Expired, got.Status, "expiry is inclusive of ExpiresAt")
}

func TestZeroTTLNeverExpires(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
		approval.WithClock(clk))
	ctx := context.Background()

	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	clk.Set(fixedNow.Add(10 * 365 * 24 * time.Hour))
	got, err := m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status)
}

func TestListDropsRecordsExpiredOutOfTheFilter(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()

	_, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	got, err := m.List(ctx, approval.Filter{Statuses: []approval.Status{approval.Pending}})
	require.NoError(t, err)
	require.Len(t, got, 1)

	clk.Set(fixedNow.Add(2 * time.Hour))
	got, err = m.List(ctx, approval.Filter{Statuses: []approval.Status{approval.Pending}})
	require.NoError(t, err)
	assert.Empty(t, got, "the stored row is still Pending but reads as Expired")

	// Unfiltered, it still comes back — carrying the derived status.
	got, err = m.List(ctx, approval.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, approval.Expired, got[0].Status)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `m.List undefined (type *approval.Manager has no field or method List)`.

- [ ] **Step 3: Write the implementation**

Append to `ops/approval/manager.go`:

```go
// List returns requests matching f, newest first, with expiry applied to
// each record.
//
// Filter.Statuses matches the STORED status, so a record that has expired
// out of the requested set is dropped after the fact: List returns UP TO
// Limit records, and a Statuses filter may yield fewer than the store
// matched. Query with no Statuses to see expired records with their derived
// status.
func (m *Manager) List(ctx context.Context, f Filter) ([]Request, error) {
	tenant, err := m.scoped(ctx, f.Tenant)
	if err != nil {
		return nil, err
	}
	f.Tenant = tenant

	out, err := m.store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	kept := out[:0]
	for i := range out {
		m.applyExpiry(&out[i])
		if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, out[i].Status) {
			continue
		}
		kept = append(kept, out[i])
	}
	return kept, nil
}

// mutate is the one concurrency primitive every transition rides: read the
// current record, derive its effective status, let fn validate and apply
// the transition, then compare-and-swap it back.
//
// fn is re-run from a FRESH read on every conflict retry — never from the
// previous attempt's copy. That re-validation is load-bearing: without it a
// vote that lost a race could be applied a second time on top of the
// winner's write, pushing a request past quorum with one approver counted
// twice.
func (m *Manager) mutate(ctx context.Context, reqID id.UUID, fn func(r *Request) error) (Request, error) {
	tenant, err := m.scoped(ctx, "")
	if err != nil {
		return Request{}, err
	}
	for attempt := 0; ; attempt++ {
		r, err := m.store.Get(ctx, reqID)
		if err != nil {
			return Request{}, err
		}
		// Report a foreign-tenant request as missing rather than forbidden,
		// so cross-tenant existence cannot be probed.
		if tenant != "" && r.Tenant != tenant {
			return Request{}, ErrNotFound
		}
		expect := r.Version
		m.applyExpiry(&r)

		if err := fn(&r); err != nil {
			return Request{}, err
		}

		err = m.store.Update(ctx, r, expect)
		switch {
		case err == nil:
			r.Version = expect + 1
			return r, nil
		case !errors.Is(err, ErrConflict):
			return Request{}, err
		case attempt >= m.cfg.maxRetries:
			return Request{}, ErrConflict
		}
	}
}

// statusErr maps a non-Pending status to the sentinel that explains it.
func statusErr(s Status) error {
	if s == Expired {
		return ErrExpired
	}
	return ErrNotPending
}
```

Update the import block of `ops/approval/manager.go` to:

```go
import (
	"context"
	"errors"
	"slices"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): list with lazy expiry and the CAS retry loop"
```

---

## Task 6: Approve, Reject, and the structural invariants

**Files:**
- Create: `ops/approval/decide.go`
- Test: `ops/approval/decide_test.go`

**Interfaces:**
- Consumes: `mutate`, `statusErr`, `Actor`, `Decision`, `Vote` from Tasks 1–5.
- Produces: `(*Manager).Approve(ctx, reqID, Actor) (Request, error)` and `(*Manager).Reject(ctx, reqID, Actor) (Request, error)`. Task 7 adds `Cancel` to the same file.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/decide_test.go`:

```go
package approval_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

// actor builds an Actor for subject id.
func actor(id string) approval.Actor {
	return approval.Actor{Subject: access.Subject{ID: id}}
}

// submitted returns a fresh manager and one Pending request.
func submitted(t *testing.T, p approval.Policy, extra ...approval.Option) (*approval.Manager, approval.Request) {
	t.Helper()
	m := newManager(t, p, extra...)
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "invoice #4471"})
	require.NoError(t, err)
	return m, r
}

func TestApproveReachesQuorum(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	ctx := context.Background()

	got, err := m.Approve(ctx, r.ID, approval.Actor{
		Subject: access.Subject{ID: "bob"}, Reason: "matched to invoice"})
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status, "1 of 2")
	require.Len(t, got.Decisions, 1)
	assert.Equal(t, "bob", got.Decisions[0].Approver)
	assert.Equal(t, approval.VoteApprove, got.Decisions[0].Vote)
	assert.Equal(t, "matched to invoice", got.Decisions[0].Reason)
	assert.True(t, got.DecidedAt.IsZero(), "not decided until quorum")

	got, err = m.Approve(ctx, r.ID, actor("carol"))
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status, "2 of 2")
	assert.Len(t, got.Decisions, 2)
	assert.True(t, fixedNow.Equal(got.DecidedAt))
	assert.Equal(t, int64(3), got.Version, "one version bump per vote")
}

func TestRejectIsTerminalAtOneVote(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 3})
	ctx := context.Background()

	got, err := m.Reject(ctx, r.ID, approval.Actor{
		Subject: access.Subject{ID: "bob"}, Reason: "duplicate payment"})
	require.NoError(t, err)
	assert.Equal(t, approval.Rejected, got.Status, "one reject ends it regardless of quorum")
	assert.Equal(t, "duplicate payment", got.Decisions[0].Reason)

	_, err = m.Approve(ctx, r.ID, actor("carol"))
	assert.ErrorIs(t, err, approval.ErrNotPending, "a rejected request never becomes approved")
}

func TestSelfApprovalRejected(t *testing.T) {
	t.Parallel()
	for _, quorum := range []int{1, 2} {
		m, r := submitted(t, approval.Policy{Quorum: quorum})
		_, err := m.Approve(context.Background(), r.ID, actor("alice"))
		assert.ErrorIs(t, err, approval.ErrSelfApproval,
			"quorum %d: the maker is never a checker", quorum)

		_, err = m.Reject(context.Background(), r.ID, actor("alice"))
		assert.ErrorIs(t, err, approval.ErrSelfApproval, "quorum %d: nor may they reject", quorum)
	}
}

func TestDoubleVoteRejected(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 3})
	ctx := context.Background()

	_, err := m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	_, err = m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrAlreadyVoted)

	_, err = m.Reject(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrAlreadyVoted, "cannot switch a vote either")
}

func TestVoteRequiresActorID(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	_, err := m.Approve(context.Background(), r.ID, approval.Actor{})
	assert.ErrorIs(t, err, approval.ErrActorRequired)
}

func TestVoteOnExpiredRequest(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	clk.Set(fixedNow.Add(2 * time.Hour))
	_, err = m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrExpired)
}

func TestVoteOnUnknownRequest(t *testing.T) {
	t.Parallel()
	m := newManager(t, approval.Policy{Quorum: 2})
	_, err := m.Approve(context.Background(), id.NewUUID(), actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotFound)
}

// TestConcurrentApprovalsRespectQuorum is the test this whole package
// exists for: many checkers voting at once must produce exactly one
// Approved transition and exactly one recorded vote per approver.
func TestConcurrentApprovalsRespectQuorum(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2},
		approval.WithMaxRetries(20))
	ctx := context.Background()

	approvers := []string{"bob", "carol", "dave", "erin", "frank"}
	var wg sync.WaitGroup
	errs := make([]error, len(approvers))
	for i, name := range approvers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = m.Approve(ctx, r.ID, actor(name))
		}()
	}
	wg.Wait()

	got, err := m.Get(ctx, r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)
	assert.Len(t, got.Decisions, 2, "voting stops at quorum; late voters are refused")

	seen := map[string]bool{}
	for _, d := range got.Decisions {
		assert.False(t, seen[d.Approver], "approver %s counted twice", d.Approver)
		seen[d.Approver] = true
	}

	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case assert.ErrorIs(t, err, approval.ErrNotPending):
			refused++
		}
	}
	assert.Equal(t, 2, ok, "exactly quorum many votes succeed")
	assert.Equal(t, len(approvers)-2, refused)
}
```

Add `"github.com/dmitrymomot/forge/core/id"` to this file's imports for `TestVoteOnUnknownRequest`.

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `m.Approve undefined`, `m.Reject undefined`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/decide.go`:

```go
package approval

import (
	"context"

	"github.com/dmitrymomot/forge/core/id"
)

// Approve records an approving decision from a. When it is the quorum-th
// distinct approval the request becomes Approved.
//
// It refuses the request's own requester (ErrSelfApproval) at every quorum
// — including quorum 1, which is still dual control — and refuses a second
// decision from an approver who has already voted (ErrAlreadyVoted).
func (m *Manager) Approve(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	return m.vote(ctx, reqID, a, VoteApprove)
}

// Reject records a rejecting decision from a. A single rejection is
// terminal: there is no reject quorum, because one checker seeing a problem
// is reason enough to stop.
func (m *Manager) Reject(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	return m.vote(ctx, reqID, a, VoteReject)
}

func (m *Manager) vote(ctx context.Context, reqID id.UUID, a Actor, v Vote) (Request, error) {
	if a.Subject.ID == "" {
		return Request{}, ErrActorRequired
	}
	action := actionApprove
	if v == VoteReject {
		action = actionReject
	}

	var denied bool
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if r.Status != Pending {
			return statusErr(r.Status)
		}
		if r.Requester == a.Subject.ID {
			return ErrSelfApproval
		}
		for i := range r.Decisions {
			if r.Decisions[i].Approver == a.Subject.ID {
				return ErrAlreadyVoted
			}
		}
		// The decider runs last: it may hit a database or an org chart, so
		// it only pays off once the vote is otherwise legal.
		if err := m.eligible(ctx, *r, a, verbDecide); err != nil {
			denied = true
			return err
		}

		now := m.now()
		r.Decisions = append(r.Decisions, Decision{
			At:       now,
			Approver: a.Subject.ID,
			Reason:   a.Reason,
			Vote:     v,
		})
		pol, ok := m.policyFor(r.Kind)
		if !ok {
			return ErrUnknownKind
		}
		switch {
		case v == VoteReject:
			r.Status = Rejected
			r.DecidedAt = now
		case r.Approvals() >= pol.Quorum:
			r.Status = Approved
			r.DecidedAt = now
		}
		return nil
	})
	if err != nil {
		if denied {
			// An ineligible actor trying to push a request through is the
			// most security-relevant event this package sees. Record it,
			// and let the audit error win only if the caller has no error
			// of their own to act on.
			if aerr := m.auditDenied(ctx, reqID, action, a.Subject.ID, err); aerr != nil {
				return Request{}, aerr
			}
		}
		return Request{}, err
	}
	return r, m.audit(ctx, r, action, a.Subject.ID, outcomeSuccess, a.Reason)
}
```

`m.eligible`, `verbDecide`, `m.auditDenied`, `actionApprove`, and `actionReject` do not exist yet. Add stubs so this compiles — Task 9 fills `eligible`, Task 10 fills the audit funcs. Append to `ops/approval/manager.go`:

```go
// Audit action names.
const (
	actionApprove = "approval.approve"
	actionReject  = "approval.reject"
)

// Eligibility verbs, appended to a kind name to form the access.Action.
const verbDecide = "decide"

// eligible checks the actor against the decider seam. Landed in Task 9;
// until then every actor is eligible.
func (m *Manager) eligible(_ context.Context, _ Request, _ Actor, _ string) error {
	return nil
}

// auditDenied records a refused attempt. Landed in Task 10.
func (m *Manager) auditDenied(_ context.Context, _ id.UUID, _, _ string, _ error) error {
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS, including `TestConcurrentApprovalsRespectQuorum` under `-race`.

Run it 20 times to shake out ordering flakes:
`go test -race -count=20 -run TestConcurrentApprovals ./ops/approval/`
Expected: `ok` — 20 consecutive passes.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): approve and reject with quorum and dual-control invariants"
```

---

## Task 7: Cancel

**Files:**
- Modify: `ops/approval/decide.go`
- Test: `ops/approval/cancel_test.go`

**Interfaces:**
- Consumes: `mutate`, `Actor`, `eligible` from Tasks 5–6.
- Produces: `(*Manager).Cancel(ctx, reqID, Actor) (Request, error)` and the `verbCancel` eligibility verb consumed by Task 9.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/cancel_test.go`:

```go
package approval_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/approval"
)

func TestCancelFromPending(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})

	got, err := m.Cancel(context.Background(), r.ID,
		approval.Actor{Subject: access.Subject{ID: "alice"}, Reason: "wrong vendor"})
	require.NoError(t, err)
	assert.Equal(t, approval.Cancelled, got.Status)
	assert.True(t, fixedNow.Equal(got.DecidedAt))
}

func TestCancelFromApproved(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	_, err := m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	got, err := m.Cancel(ctx, r.ID, actor("alice"))
	require.NoError(t, err)
	assert.Equal(t, approval.Cancelled, got.Status, "approved but unexecuted is still cancellable")
}

func TestCancelRejectedOnTerminalRequest(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	ctx := context.Background()

	_, err := m.Reject(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	_, err = m.Cancel(ctx, r.ID, actor("alice"))
	assert.ErrorIs(t, err, approval.ErrNotCancellable)
}

func TestCancelRequiresActorID(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	_, err := m.Cancel(context.Background(), r.ID, approval.Actor{})
	assert.ErrorIs(t, err, approval.ErrActorRequired)
}
```

Add `"github.com/dmitrymomot/forge/auth/access"` to this file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `m.Cancel undefined`.

- [ ] **Step 3: Write the implementation**

Append to `ops/approval/decide.go`:

```go
// Cancel withdraws a request before it executes. It is legal from Pending
// and Approved but not from Executing — an in-flight action is not
// cancellable; the executor reports the outcome with Complete or Fail.
//
// The requester may always cancel their own request. Any other actor is
// gated on "<kind>:cancel" when a decider is configured: withdrawing
// someone else's request is a different privilege from judging it, and a
// policy that grants one need not grant the other.
func (m *Manager) Cancel(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	if a.Subject.ID == "" {
		return Request{}, ErrActorRequired
	}

	var denied bool
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if r.Status != Pending && r.Status != Approved {
			if r.Status == Expired {
				return ErrExpired
			}
			return ErrNotCancellable
		}
		if r.Requester != a.Subject.ID {
			if err := m.eligible(ctx, *r, a, verbCancel); err != nil {
				denied = true
				return err
			}
		}
		r.Status = Cancelled
		r.DecidedAt = m.now()
		return nil
	})
	if err != nil {
		if denied {
			if aerr := m.auditDenied(ctx, reqID, actionCancel, a.Subject.ID, err); aerr != nil {
				return Request{}, aerr
			}
		}
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionCancel, a.Subject.ID, outcomeSuccess, a.Reason)
}
```

Append the new constants to `ops/approval/manager.go`'s const blocks:

```go
const actionCancel = "approval.cancel"

const verbCancel = "cancel"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): cancel with requester short-circuit"
```

---

## Task 8: Claim, Complete, Fail, Release, and Execute

**Files:**
- Create: `ops/approval/execute.go`
- Test: `ops/approval/execute_test.go`

**Interfaces:**
- Consumes: `mutate`, `Policy.ClaimTTL`, sentinels from Tasks 1–7.
- Produces: `(*Manager).Claim`, `Complete`, `Fail`, `Release`, `Execute`.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/execute_test.go`:

```go
package approval_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

// approved returns a manager and a request that has reached quorum.
func approved(t *testing.T, p approval.Policy, extra ...approval.Option) (*approval.Manager, approval.Request) {
	t.Helper()
	m, r := submitted(t, p, extra...)
	got, err := m.Approve(context.Background(), r.ID, actor("bob"))
	require.NoError(t, err)
	require.Equal(t, approval.Approved, got.Status)
	return m, got
}

func TestClaimAndComplete(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	got, err := m.Claim(ctx, r.ID, "worker-3")
	require.NoError(t, err)
	assert.Equal(t, approval.Executing, got.Status)
	assert.Equal(t, "worker-3", got.ClaimedBy)
	assert.True(t, fixedNow.Equal(got.ClaimedAt))

	got, err = m.Complete(ctx, r.ID, "worker-3")
	require.NoError(t, err)
	assert.Equal(t, approval.Executed, got.Status)
	assert.True(t, got.Status.Terminal())
}

func TestClaimIsExclusive(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	_, err = m.Claim(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed)
}

func TestClaimRequiresApproved(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 2})
	_, err := m.Claim(context.Background(), r.ID, "worker-1")
	assert.ErrorIs(t, err, approval.ErrNotApproved, "a pending request is not executable")
}

func TestClaimRequiresExecutorID(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	_, err := m.Claim(context.Background(), r.ID, "")
	assert.ErrorIs(t, err, approval.ErrExecutorRequired)
}

func TestZeroClaimTTLWedgesUntilRelease(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m, r := approved(t, approval.Policy{Quorum: 1}, approval.WithClock(clk))
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	clk.Set(fixedNow.Add(365 * 24 * time.Hour))
	_, err = m.Claim(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed,
		"ClaimTTL 0 never lets a second executor in — the safe default for money")

	// Release is the escape hatch, and it is deliberately not holder-checked.
	got, err := m.Release(ctx, r.ID, actor("ops-oncall"))
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)
	assert.Empty(t, got.ClaimedBy)
	assert.True(t, got.ClaimedAt.IsZero())

	got, err = m.Claim(ctx, r.ID, "worker-2")
	require.NoError(t, err)
	assert.Equal(t, "worker-2", got.ClaimedBy)
}

func TestStaleClaimIsReclaimable(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m, r := approved(t, approval.Policy{Quorum: 1, ClaimTTL: 5 * time.Minute},
		approval.WithClock(clk))
	ctx := context.Background()

	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	clk.Set(fixedNow.Add(time.Minute))
	_, err = m.Claim(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed, "lease still held")

	clk.Set(fixedNow.Add(5 * time.Minute))
	got, err := m.Claim(ctx, r.ID, "worker-2")
	require.NoError(t, err, "lease expired at exactly ClaimTTL")
	assert.Equal(t, "worker-2", got.ClaimedBy)
}

func TestCompleteAndFailAreHolderChecked(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()
	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	_, err = m.Complete(ctx, r.ID, "worker-2")
	assert.ErrorIs(t, err, approval.ErrNotClaimHolder)

	_, err = m.Fail(ctx, r.ID, "worker-2", "nope")
	assert.ErrorIs(t, err, approval.ErrNotClaimHolder)
}

func TestCompleteRequiresExecuting(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	_, err := m.Complete(context.Background(), r.ID, "worker-1")
	assert.ErrorIs(t, err, approval.ErrNotExecuting)
}

func TestFailRecordsReason(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()
	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	got, err := m.Fail(ctx, r.ID, "worker-1", "gateway rejected: insufficient funds")
	require.NoError(t, err)
	assert.Equal(t, approval.Failed, got.Status)
	assert.Equal(t, "gateway rejected: insufficient funds", got.Meta["failure"])
}

func TestApprovedRequestExpiresBeforeClaim(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 1, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	_, err = m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)

	clk.Set(fixedNow.Add(2 * time.Hour))
	_, err = m.Claim(ctx, r.ID, "worker-1")
	assert.ErrorIs(t, err, approval.ErrExpired, "an approved-but-unexecuted request is not immortal")
}

func TestExecutingDoesNotExpire(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(fixedNow)
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 1, TTL: time.Hour}),
		approval.WithClock(clk))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	_, err = m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)
	_, err = m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	clk.Set(fixedNow.Add(2 * time.Hour))
	got, err := m.Complete(ctx, r.ID, "worker-1")
	require.NoError(t, err, "TTL does not reap an in-flight action; ClaimTTL governs it")
	assert.Equal(t, approval.Executed, got.Status)
}

// TestConcurrentClaimsHaveOneWinner is the execute-once guarantee.
func TestConcurrentClaimsHaveOneWinner(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1}, approval.WithMaxRetries(20))
	ctx := context.Background()

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = m.Claim(ctx, r.ID, "worker")
		}()
	}
	wg.Wait()

	var won int
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			assert.ErrorIs(t, err, approval.ErrAlreadyClaimed)
		}
	}
	assert.Equal(t, 1, won, "exactly one executor claims the action")
}

func TestExecuteWrapsTheTrio(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()

	var sawPayload payoutPayload
	got, err := m.Execute(ctx, r.ID, "worker-1", func(ctx context.Context, req approval.Request) error {
		p, err := approval.PayloadOf(kindPayout, req)
		require.NoError(t, err)
		sawPayload = p
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, approval.Executed, got.Status)
	assert.Equal(t, "po_88", sawPayload.PayoutID)
}

func TestExecuteFailsOnActionError(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	boom := errors.New("gateway down")

	got, err := m.Execute(context.Background(), r.ID, "worker-1",
		func(context.Context, approval.Request) error { return boom })
	assert.ErrorIs(t, err, boom)
	assert.Equal(t, approval.Failed, got.Status, "the failure is recorded, not swallowed")
}

func TestExecuteYieldsToTheClaimHolder(t *testing.T) {
	t.Parallel()
	m, r := approved(t, approval.Policy{Quorum: 1})
	ctx := context.Background()
	_, err := m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	var ran bool
	_, err = m.Execute(ctx, r.ID, "worker-2", func(context.Context, approval.Request) error {
		ran = true
		return nil
	})
	assert.ErrorIs(t, err, approval.ErrAlreadyClaimed)
	assert.False(t, ran, "the action must not run without the claim")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `m.Claim undefined`, `m.Complete undefined`, `m.Fail undefined`, `m.Release undefined`, `m.Execute undefined`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/execute.go`:

```go
package approval

import (
	"context"
	"errors"

	"github.com/dmitrymomot/forge/core/id"
)

// Claim takes exclusive ownership of an approved request so its action runs
// once. It is the transition that makes double execution impossible: two
// operators clicking the same button, or two webhook deliveries racing,
// produce exactly one winner and one ErrAlreadyClaimed.
//
// It is legal only from Approved, or from Executing when the current claim
// has gone stale under the kind's ClaimTTL. With ClaimTTL 0 — the default —
// a claim is never stale, so an executor that dies mid-action wedges the
// request until Release is called.
func (m *Manager) Claim(ctx context.Context, reqID id.UUID, executor string) (Request, error) {
	if executor == "" {
		return Request{}, ErrExecutorRequired
	}
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		switch r.Status {
		case Approved:
			// claimable
		case Executing:
			pol, ok := m.policyFor(r.Kind)
			if !ok {
				return ErrUnknownKind
			}
			if pol.ClaimTTL <= 0 {
				return ErrAlreadyClaimed
			}
			if m.now().Before(r.ClaimedAt.Add(pol.ClaimTTL)) {
				return ErrAlreadyClaimed
			}
		case Expired:
			return ErrExpired
		default:
			return ErrNotApproved
		}
		r.Status = Executing
		r.ClaimedBy = executor
		r.ClaimedAt = m.now()
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionClaim, executor, outcomeSuccess, "")
}

// Complete records that the claimed action succeeded. Only the executor
// holding the claim may call it.
func (m *Manager) Complete(ctx context.Context, reqID id.UUID, executor string) (Request, error) {
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if err := checkHolder(*r, executor); err != nil {
			return err
		}
		r.Status = Executed
		r.DecidedAt = m.now()
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionComplete, executor, outcomeSuccess, "")
}

// Fail records that the claimed action failed, storing reason under the
// "failure" meta key. Only the claim holder may call it. The request is
// terminal afterwards: re-running it means submitting a new request, which
// means asking for approval again.
func (m *Manager) Fail(ctx context.Context, reqID id.UUID, executor, reason string) (Request, error) {
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if err := checkHolder(*r, executor); err != nil {
			return err
		}
		r.Status = Failed
		r.DecidedAt = m.now()
		if r.Meta == nil {
			r.Meta = make(map[string]string, 1)
		}
		r.Meta["failure"] = reason
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionFail, executor, outcomeFailure, reason)
}

// Release returns a claimed request to Approved so another executor can
// take it. It is the administrative escape hatch for a request wedged by a
// dead executor, so it is deliberately NOT holder-checked — the holder is
// precisely the party that cannot call it. Gate access to it in your own
// application; every call is audited.
func (m *Manager) Release(ctx context.Context, reqID id.UUID, a Actor) (Request, error) {
	if a.Subject.ID == "" {
		return Request{}, ErrActorRequired
	}
	r, err := m.mutate(ctx, reqID, func(r *Request) error {
		if r.Status != Executing {
			return ErrNotExecuting
		}
		r.Status = Approved
		r.ClaimedBy = ""
		r.ClaimedAt = time.Time{}
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	return r, m.audit(ctx, r, actionRelease, a.Subject.ID, outcomeSuccess, a.Reason)
}

// Execute claims the request, runs fn, and reports the outcome — the
// Claim/Complete/Fail trio wired correctly so that forgetting the failure
// path cannot wedge a request.
//
// fn receives the claimed request; decode its payload with PayloadOf. A
// non-nil fn error transitions the request to Failed and is returned joined
// with any transition error. ErrAlreadyClaimed is returned unwrapped when
// another executor holds the claim, and fn never runs.
//
// fn runs exactly once per successful claim. Under a non-zero ClaimTTL a
// stale claim can be taken over, so fn may run more than once across
// executor deaths — make it idempotent, or leave ClaimTTL at 0.
func (m *Manager) Execute(ctx context.Context, reqID id.UUID, executor string, fn func(context.Context, Request) error) (Request, error) {
	r, err := m.Claim(ctx, reqID, executor)
	if err != nil {
		return Request{}, err
	}
	if err := fn(ctx, r); err != nil {
		failed, ferr := m.Fail(ctx, reqID, executor, err.Error())
		return failed, errors.Join(err, ferr)
	}
	return m.Complete(ctx, reqID, executor)
}

// checkHolder gates Complete and Fail on the claim.
func checkHolder(r Request, executor string) error {
	if r.Status != Executing {
		return ErrNotExecuting
	}
	if r.ClaimedBy != executor {
		return ErrNotClaimHolder
	}
	return nil
}
```

Add `"time"` to `execute.go`'s imports (used by `Release`).

Append the new constants to `ops/approval/manager.go`:

```go
const (
	actionClaim    = "approval.claim"
	actionComplete = "approval.complete"
	actionFail     = "approval.fail"
	actionRelease  = "approval.release"
)

const outcomeFailure = "failure"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS.

Run the race test repeatedly: `go test -race -count=20 -run TestConcurrentClaims ./ops/approval/`
Expected: `ok` — 20 consecutive passes, `won == 1` every time.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): execute-once claim lease with complete, fail, release, and execute"
```

---

## Task 9: Eligibility via the access decision seam

**Files:**
- Create: `ops/approval/eligibility.go`
- Modify: `ops/approval/manager.go` (remove the stub `eligible`), `ops/approval/options.go` (add `WithDecider`)
- Test: `ops/approval/eligibility_test.go`

**Interfaces:**
- Consumes: `Actor`, `Request`, `verbDecide`, `verbCancel`, `ErrNotEligible`.
- Produces: `WithDecider(access.Decider) Option`, and the real `(*Manager).eligible(ctx, r Request, a Actor, verb string) error`.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/eligibility_test.go`:

```go
package approval_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/ops/approval"
)

// recordingDecider captures what the manager asked, and answers with effect.
type recordingDecider struct {
	gotAction   access.Action
	gotResource access.Resource
	gotSubject  access.Subject
	effect      access.Effect
	err         error
}

func (d *recordingDecider) Decide(_ context.Context, s access.Subject, a access.Action, r access.Resource) (access.Decision, error) {
	d.gotSubject, d.gotAction, d.gotResource = s, a, r
	if d.err != nil {
		return access.Decision{}, d.err
	}
	return access.Decision{Effect: d.effect, Reason: "test"}, nil
}

func TestEligibilityAllows(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Allow}
	m, r := submitted(t, approval.Policy{Quorum: 1}, approval.WithDecider(d))

	got, err := m.Approve(context.Background(), r.ID, approval.Actor{
		Subject: access.Subject{ID: "bob", Roles: []string{"finance"}}})
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)

	assert.Equal(t, access.Action("payout.release:decide"), d.gotAction)
	assert.Equal(t, "bob", d.gotSubject.ID)
	assert.Equal(t, []string{"finance"}, d.gotSubject.Roles)
	assert.Equal(t, "approval", d.gotResource.Type)
	assert.Equal(t, r.ID.String(), d.gotResource.ID)
	assert.Equal(t, "payout.release", d.gotResource.Attrs["kind"])
	assert.Equal(t, "alice", d.gotResource.Attrs["requester"],
		"relational policies need the requester")

	raw, ok := d.gotResource.Attrs["payload"].(json.RawMessage)
	require.True(t, ok, "value-aware policies need the raw payload")
	assert.JSONEq(t, `{"payout_id":"po_88","amount":250000}`, string(raw))
}

func TestEligibilityDenies(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 1}, approval.WithDecider(d))

	_, err := m.Approve(context.Background(), r.ID, actor("dave"))
	assert.ErrorIs(t, err, approval.ErrNotEligible)

	got, err := m.Get(context.Background(), r.ID)
	require.NoError(t, err)
	assert.Equal(t, approval.Pending, got.Status, "a denied vote leaves no trace on the record")
	assert.Empty(t, got.Decisions)
}

func TestEligibilityFailsClosedOnDeciderError(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{err: errors.New("org chart unreachable")}
	m, r := submitted(t, approval.Policy{Quorum: 1}, approval.WithDecider(d))

	_, err := m.Approve(context.Background(), r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotEligible,
		"an unavailable decider must never grant approval")
}

func TestEligibilityGatesRejectToo(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	_, err := m.Reject(context.Background(), r.ID, actor("mallory"))
	assert.ErrorIs(t, err, approval.ErrNotEligible,
		"an ungated reject lets anyone DoS the approval queue")
	assert.Equal(t, access.Action("payout.release:decide"), d.gotAction)
}

func TestEligibilityRunsAfterCheapInvariants(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Allow}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	_, err := m.Approve(context.Background(), r.ID, actor("alice"))
	require.ErrorIs(t, err, approval.ErrSelfApproval)
	assert.Empty(t, d.gotAction, "self-approval is refused without consulting the decider")
}

func TestCancelUsesCancelVerbForNonRequester(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Allow}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	_, err := m.Cancel(context.Background(), r.ID, actor("ops-oncall"))
	require.NoError(t, err)
	assert.Equal(t, access.Action("payout.release:cancel"), d.gotAction)
}

func TestCancelByRequesterSkipsTheDecider(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 2}, approval.WithDecider(d))

	got, err := m.Cancel(context.Background(), r.ID, actor("alice"))
	require.NoError(t, err, "withdrawing your own request is never gated")
	assert.Equal(t, approval.Cancelled, got.Status)
	assert.Empty(t, d.gotAction)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `undefined: approval.WithDecider`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/eligibility.go`:

```go
package approval

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
)

// eligible asks the decision seam whether a may act on r.
//
// The action is "<kind>:<verb>" — "payout.release:decide" for a vote,
// "payout.release:cancel" for a third-party withdrawal. Both votes share
// one verb: being a checker is the privilege, and which way a checker votes
// is not a separate grant.
//
// It fails closed. access.Authorize already collapses Abstain and decider
// errors into Deny, so anything short of an explicit Allow refuses the
// action. A decider that cannot reach its data must never let a payout
// through.
func (m *Manager) eligible(ctx context.Context, r Request, a Actor, verb string) error {
	if m.cfg.decider == nil {
		return nil
	}
	res := access.Resource{
		Type:   "approval",
		ID:     r.ID.String(),
		Tenant: r.Tenant,
		Attrs: map[string]any{
			"kind":      r.Kind,
			"requester": r.Requester,
			// The raw payload, not a decoded map: value-aware rules decode
			// it themselves, and rules that ignore it pay nothing.
			"payload": r.Payload,
		},
	}
	dec, err := access.Authorize(ctx, m.cfg.decider, a.Subject, access.Action(r.Kind+":"+verb), res)
	if err != nil {
		// Never surface the decider's raw error to the caller: it may carry
		// internal detail, and the caller sees a generic refusal either way.
		return fmt.Errorf("%w: %s", ErrNotEligible, "eligibility could not be established")
	}
	if dec.Effect != access.Allow {
		return ErrNotEligible
	}
	return nil
}
```

Delete the stub `eligible` from `ops/approval/manager.go`.

Add to `ops/approval/options.go`'s `config` struct and options:

```go
// in config:
	decider access.Decider

// WithDecider gates who may decide on a request. The manager asks it
// "<kind>:decide" before recording any vote and "<kind>:cancel" before a
// non-requester cancellation, passing the request's kind, requester, and
// raw payload as resource attributes so relational rules ("must be the
// requester's manager") and value-aware rules ("over 10 days needs the
// department head") both work.
//
// Without it, any principal other than the requester may decide — the
// correct single-team default. The structural invariants (no self-approval,
// one vote per approver) hold either way.
func WithDecider(d access.Decider) Option {
	return func(c *config) { c.decider = d }
}
```

Add `"github.com/dmitrymomot/forge/auth/access"` to `options.go`'s imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): approver eligibility over the access decision seam"
```

---

## Task 10: Audit trail

**Files:**
- Create: `ops/approval/audit.go`
- Modify: `ops/approval/manager.go` (remove the `audit`/`auditDenied` stubs), `ops/approval/options.go` (add `WithAuditor`)
- Test: `ops/approval/audit_test.go`

**Interfaces:**
- Consumes: every `action*`/`outcome*` constant, `Request`, `ErrAuditFailed`.
- Produces: `WithAuditor(*auditlog.Recorder) Option`, and the real `(*Manager).audit(ctx, r Request, action, actor, outcome, reason string) error` plus `(*Manager).auditDenied(ctx, reqID id.UUID, action, actor string, cause error) error`.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/audit_test.go`:

```go
package approval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

// failingSink rejects every write.
type failingSink struct{}

func (failingSink) Write(context.Context, auditlog.Event) error {
	return errors.New("sink unavailable")
}

func TestAuditRecordsEveryTransition(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	m, r := submitted(t, approval.Policy{Quorum: 1},
		approval.WithAuditor(auditlog.New(sink)))
	ctx := context.Background()

	_, err := m.Approve(ctx, r.ID, actor("bob"))
	require.NoError(t, err)
	_, err = m.Claim(ctx, r.ID, "worker-1")
	require.NoError(t, err)
	_, err = m.Complete(ctx, r.ID, "worker-1")
	require.NoError(t, err)

	events := sink.Events()
	require.Len(t, events, 4)

	assert.Equal(t, "approval.submit", events[0].Action)
	assert.Equal(t, "alice", events[0].Actor)
	assert.Equal(t, auditlog.OutcomeSuccess, events[0].Outcome)
	assert.Equal(t, "approval:"+r.ID.String(), events[0].Resource)
	assert.Equal(t, "payout.release", events[0].Meta["kind"])

	assert.Equal(t, "approval.approve", events[1].Action)
	assert.Equal(t, "bob", events[1].Actor)
	assert.Equal(t, "approved", events[1].Meta["status"])

	assert.Equal(t, "approval.claim", events[2].Action)
	assert.Equal(t, "worker-1", events[2].Actor)

	assert.Equal(t, "approval.complete", events[3].Action)
}

func TestAuditRecordsDeniedAttempts(t *testing.T) {
	t.Parallel()
	sink := auditlog.NewMemorySink()
	d := &recordingDecider{effect: access.Deny}
	m, r := submitted(t, approval.Policy{Quorum: 1},
		approval.WithDecider(d), approval.WithAuditor(auditlog.New(sink)))

	_, err := m.Approve(context.Background(), r.ID, actor("mallory"))
	require.ErrorIs(t, err, approval.ErrNotEligible)

	events := sink.Events()
	require.Len(t, events, 2, "submit + the denied attempt")
	denied := events[1]
	assert.Equal(t, "approval.approve", denied.Action)
	assert.Equal(t, auditlog.OutcomeDenied, denied.Outcome)
	assert.Equal(t, "mallory", denied.Actor)
	assert.Equal(t, "approval:"+r.ID.String(), denied.Resource)
}

func TestAuditFailureReturnsDurableRequest(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1},
		approval.WithAuditor(auditlog.New(failingSink{})))
	ctx := context.Background()

	got, err := m.Approve(ctx, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrAuditFailed)
	assert.Equal(t, approval.Approved, got.Status,
		"the transition is durable even though the trail write failed")

	stored, gerr := m.Get(ctx, r.ID)
	require.NoError(t, gerr)
	assert.Equal(t, approval.Approved, stored.Status, "and it really is persisted")
}

func TestNoAuditorIsSilent(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1})
	_, err := m.Approve(context.Background(), r.ID, actor("bob"))
	assert.NoError(t, err, "the auditor seam is optional")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `undefined: approval.WithAuditor`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/audit.go`:

```go
package approval

import (
	"context"
	"fmt"
	"strconv"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/auditlog"
)

// audit records a completed transition.
//
// It runs AFTER the store write, never before: auditing first would record
// transitions that a lost CAS race then discarded. The cost of that
// ordering is that a failed trail write cannot undo a durable transition,
// so the caller gets the updated Request AND an ErrAuditFailed-wrapped
// error. Match it with errors.Is and alert — in a dual-control package a
// silent gap in the trail is the failure mode that matters.
func (m *Manager) audit(ctx context.Context, r Request, action, actor, outcome, reason string) error {
	if m.cfg.auditor == nil {
		return nil
	}
	meta := map[string]string{
		"kind":   r.Kind,
		"status": r.Status.String(),
	}
	if reason != "" {
		meta["reason"] = reason
	}
	if action == actionApprove || action == actionReject {
		if pol, ok := m.policyFor(r.Kind); ok {
			meta["quorum"] = strconv.Itoa(pol.Quorum)
			meta["approvals"] = strconv.Itoa(r.Approvals())
		}
	}
	_, err := m.cfg.auditor.Record(ctx, auditlog.Event{
		Actor:    actor,
		Action:   action,
		Resource: "approval:" + r.ID.String(),
		Outcome:  auditlog.Outcome(outcome),
		Tenant:   r.Tenant,
		Meta:     meta,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuditFailed, err)
	}
	return nil
}

// auditDenied records a refused attempt. An ineligible actor trying to push
// a request through is the most security-relevant event this package sees,
// so it is never invisible — even though no state changed.
func (m *Manager) auditDenied(ctx context.Context, reqID id.UUID, action, actor string, cause error) error {
	if m.cfg.auditor == nil {
		return nil
	}
	_, err := m.cfg.auditor.Record(ctx, auditlog.Event{
		Actor:    actor,
		Action:   action,
		Resource: "approval:" + reqID.String(),
		Outcome:  auditlog.OutcomeDenied,
		Meta:     map[string]string{"cause": cause.Error()},
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuditFailed, err)
	}
	return nil
}
```

Delete the stub `audit` and `auditDenied` from `ops/approval/manager.go`, and change the `outcomeSuccess`/`outcomeFailure` constants there to match auditlog's values:

```go
const (
	outcomeSuccess = string(auditlog.OutcomeSuccess)
	outcomeFailure = string(auditlog.OutcomeFailure)
)
```

Add to `ops/approval/options.go`:

```go
// in config:
	auditor *auditlog.Recorder

// WithAuditor records every state change to the audit trail: submissions,
// decisions, cancellations, claims, and outcomes — plus every attempt a
// decider refused, as OutcomeDenied.
//
// The trail is written after the state change is durable. If the sink
// fails, the transition still happened and the operation returns
// ErrAuditFailed alongside the updated request.
func WithAuditor(rec *auditlog.Recorder) Option {
	return func(c *config) { c.auditor = rec }
}
```

Add `"github.com/dmitrymomot/forge/ops/auditlog"` to `options.go` and `manager.go` imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): audit trail with denied-attempt recording"
```

---

## Task 11: Tenant scoping

**Files:**
- Modify: `ops/approval/manager.go` (replace the stub `scoped`), `ops/approval/options.go` (add `WithScope`)
- Test: `ops/approval/tenancy_test.go`

**Interfaces:**
- Consumes: `ErrScope`, `ErrNotFound`, `mutate`, `List`, `Submit`.
- Produces: `WithScope(func(context.Context) (string, error)) Option` and the real `(*Manager).scoped(ctx, requested string) (string, error)`.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/tenancy_test.go`:

```go
package approval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

type tenantKey struct{}

func ctxTenant(t string) context.Context {
	return context.WithValue(context.Background(), tenantKey{}, t)
}

func tenantFromCtx(ctx context.Context) (string, error) {
	t, _ := ctx.Value(tenantKey{}).(string)
	return t, nil
}

func scopedManager(t *testing.T, store approval.Store) *approval.Manager {
	t.Helper()
	return approval.New(store,
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
		approval.WithClock(clock.NewMock(fixedNow)),
		approval.WithScope(tenantFromCtx))
}

func TestScopeStampsTenantOnSubmit(t *testing.T) {
	t.Parallel()
	m := scopedManager(t, approval.NewMemoryStore())

	r, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "acme", r.Tenant)
}

func TestScopeFailsClosedOnEmptyTenant(t *testing.T) {
	t.Parallel()
	m := scopedManager(t, approval.NewMemoryStore())

	_, err := approval.Submit(context.Background(), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	assert.ErrorIs(t, err, approval.ErrScope, "a missing tenant must not fall back to global")
}

func TestScopeFailsClosedOnHookError(t *testing.T) {
	t.Parallel()
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindPayout, approval.Policy{Quorum: 2}),
		approval.WithScope(func(context.Context) (string, error) {
			return "", errors.New("tenant lookup failed")
		}))

	_, err := approval.Submit(context.Background(), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	assert.ErrorIs(t, err, approval.ErrScope)
}

func TestScopeRejectsDisagreeingExplicitTenant(t *testing.T) {
	t.Parallel()
	m := scopedManager(t, approval.NewMemoryStore())

	_, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice", Tenant: "globex"})
	assert.ErrorIs(t, err, approval.ErrScope)

	// Agreeing is fine.
	_, err = approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice", Tenant: "acme"})
	assert.NoError(t, err)
}

func TestCrossTenantAccessReportsNotFound(t *testing.T) {
	t.Parallel()
	store := approval.NewMemoryStore()
	m := scopedManager(t, store)

	r, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)

	other := ctxTenant("globex")
	_, err = m.Approve(other, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotFound,
		"not ErrScope — cross-tenant existence must not be probeable")

	_, err = m.Cancel(other, r.ID, actor("bob"))
	assert.ErrorIs(t, err, approval.ErrNotFound)
}

func TestListIsConfinedToScope(t *testing.T) {
	t.Parallel()
	store := approval.NewMemoryStore()
	m := scopedManager(t, store)

	_, err := approval.Submit(ctxTenant("acme"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	require.NoError(t, err)
	_, err = approval.Submit(ctxTenant("globex"), m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "carol"})
	require.NoError(t, err)

	got, err := m.List(ctxTenant("acme"), approval.Filter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "acme", got[0].Tenant)
}

func TestUnscopedManagerPaysNoCeremony(t *testing.T) {
	t.Parallel()
	m, r := submitted(t, approval.Policy{Quorum: 1})
	assert.Empty(t, r.Tenant)

	got, err := m.Approve(context.Background(), r.ID, actor("bob"))
	require.NoError(t, err)
	assert.Equal(t, approval.Approved, got.Status)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL — `undefined: approval.WithScope`.

- [ ] **Step 3: Write the implementation**

Replace the stub `scoped` in `ops/approval/manager.go`:

```go
// scoped resolves the tenant an operation is confined to.
//
// With no WithScope hook it passes the requested tenant through, so
// single-tenant applications pay nothing. With one, it fails closed: a hook
// error or an empty tenant aborts with ErrScope rather than silently
// operating across every tenant, and an explicitly requested tenant must
// agree with the scoped one.
func (m *Manager) scoped(ctx context.Context, requested string) (string, error) {
	if m.cfg.scope == nil {
		return requested, nil
	}
	tenant, err := m.cfg.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if tenant == "" {
		return "", ErrScope
	}
	if requested != "" && requested != tenant {
		return "", fmt.Errorf("%w: requested tenant %q is outside scope %q", ErrScope, requested, tenant)
	}
	return tenant, nil
}
```

Add `"fmt"` to `manager.go`'s imports.

Add to `ops/approval/options.go`:

```go
// in config:
	scope func(context.Context) (string, error)

// WithScope derives the tenant from context for every operation: Submit
// stamps it, List is confined to it, and Get and every transition report
// ErrNotFound for other tenants' requests — not a forbidden error, so
// cross-tenant existence cannot be probed.
//
// Fail-closed: a hook error or an empty tenant fails the operation with
// ErrScope. A nil fn leaves the manager unscoped, which is the correct
// single-tenant default.
func WithScope(fn func(context.Context) (string, error)) Option {
	return func(c *config) { c.scope = fn }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./ops/approval/... && just test ./ops/approval/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): fail-closed tenant scoping seam"
```

---

## Task 12: Postgres store

**Files:**
- Create: `ops/approval/pgstore/migrations/00001_create_forge_approval_requests.sql`
- Create: `ops/approval/pgstore/pgstore.go`
- Create: `ops/approval/pgstore/doc.go`
- Test: `ops/approval/pgstore/pgstore_test.go`

**Interfaces:**
- Consumes: `approval.Store`, `approval.Request`, `approval.Filter`, sentinels, `approvaltest.Run`.
- Produces: `pgstore.New(pool *pgxpool.Pool) *Store` implementing `approval.Store`, and `pgstore.Migrations fs.FS`.

Read `auth/apikey/pgstore/pgstore.go` and `auth/apikey/pgstore/pgstore_test.go` in full before starting — this task mirrors both exactly.

Note the setup chain, which is easy to get wrong: `pgtest` exposes only
`DSN(tb)`, so the pool comes from `postgres.Open`, and `migration.Up` takes
a `*sql.DB` (via `stdlib.OpenDBFromPool`), not the pool. The version table
is set with `migration.WithTable`, and it must be unique — a colliding
group name silently skips the migration and every test then fails on a
missing table.

- [ ] **Step 1: Write the failing test**

Create `ops/approval/pgstore/pgstore_test.go`:

```go
//go:build integration

package pgstore_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/approval/approvaltest"
	"github.com/dmitrymomot/forge/ops/approval/pgstore"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ approval.Store = (*pgstore.Store)(nil)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations,
		migration.WithTable("forge_approval_schema")).Up(context.Background(), db))
	return pool
}

// TestPgStoreContract runs the same suite the memory store passes. The
// table outlives the process, so the suite namespaces its own fixtures —
// nothing here truncates.
func TestPgStoreContract(t *testing.T) {
	pool := newPool(t)
	approvaltest.Run(t, func(t *testing.T) approval.Store {
		return pgstore.New(pool)
	})
}

// TestConcurrentUpdatesConflict proves the CAS is enforced by Postgres
// itself, not by a mutex the memory store happens to hold.
func TestConcurrentUpdatesConflict(t *testing.T) {
	s := pgstore.New(newPool(t))
	ctx := context.Background()

	r := approval.Request{
		ID:        id.NewUUID(),
		Kind:      "kind-" + id.NewUUID().String(),
		Tenant:    "tenant-" + id.NewUUID().String(),
		Requester: "alice",
		Status:    approval.Pending,
		Version:   1,
		Payload:   json.RawMessage(`{}`),
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	require.NoError(t, s.Create(ctx, r))

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Update(ctx, r, 1)
		}()
	}
	wg.Wait()

	var won int
	for _, err := range errs {
		if err == nil {
			won++
			continue
		}
		require.ErrorIs(t, err, approval.ErrConflict)
	}
	require.Equal(t, 1, won, "exactly one CAS wins at a given version")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test-integration ./ops/approval/...`
Expected: FAIL — `no required module provides package .../ops/approval/pgstore`.

- [ ] **Step 3: Write the implementation**

Create `ops/approval/pgstore/migrations/00001_create_forge_approval_requests.sql`:

```sql
-- +goose Up
CREATE TABLE forge_approval_requests (
    id          uuid PRIMARY KEY,
    kind        text NOT NULL,
    tenant      text NOT NULL DEFAULT '',
    requester   text NOT NULL,
    reason      text NOT NULL DEFAULT '',
    status      smallint NOT NULL,
    version     bigint NOT NULL DEFAULT 1,
    payload     json NOT NULL,
    decisions   json NOT NULL DEFAULT '[]'::json,
    meta        jsonb NOT NULL DEFAULT '{}'::jsonb,
    claimed_by  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL,
    expires_at  timestamptz,
    claimed_at  timestamptz,
    decided_at  timestamptz
);

-- Covers the Filter WHERE columns in filter order, then id for the
-- newest-first ordering. An index that does not cover the filter is an
-- index Postgres will not use.
CREATE INDEX forge_approval_requests_list_idx
    ON forge_approval_requests (tenant, kind, status, id DESC);

-- Partial: rows with no expiry are never selected by an expiry bound, so
-- they do not belong in this index.
CREATE INDEX forge_approval_requests_expiry_idx
    ON forge_approval_requests (status, expires_at)
    WHERE expires_at IS NOT NULL;

-- +goose Down
DROP TABLE forge_approval_requests;
```

Create `ops/approval/pgstore/pgstore.go`. `payload` and `decisions` are
`json`, not `jsonb`: neither is queried by content, and `json` round-trips
the exact bytes with no reparse — so a payload a consumer hashes stays
byte-identical.

```go
package pgstore

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_approval_requests,
// rooted so its .sql files sit at fsys root (data/migration.New globs
// fsys's root, not subdirectories). Apply it under its own version table
// (migration.WithTable("forge_approval_schema")) — a colliding group name
// silently skips.
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of approval.Store. The pool's
// lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var _ approval.Store = (*Store)(nil)

// New builds a Postgres approval Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const cols = `id, kind, tenant, requester, reason, status, version, payload, decisions, meta, claimed_by, created_at, expires_at, claimed_at, decided_at`

const createSQL = `
INSERT INTO forge_approval_requests (` + cols + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

// Create inserts r. A colliding id yields approval.ErrDuplicate.
func (s *Store) Create(ctx context.Context, r approval.Request) error {
	decisions, meta, err := encodeState(r)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, createSQL,
		r.ID, r.Kind, r.Tenant, r.Requester, r.Reason, int16(r.Status), r.Version,
		[]byte(r.Payload), decisions, meta, r.ClaimedBy,
		r.CreatedAt, nullTime(r.ExpiresAt), nullTime(r.ClaimedAt), nullTime(r.DecidedAt))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return approval.ErrDuplicate
	}
	return err
}

// Get returns the request for reqID, or approval.ErrNotFound.
func (s *Store) Get(ctx context.Context, reqID id.UUID) (approval.Request, error) {
	return scanRequest(s.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM forge_approval_requests WHERE id = $1`, reqID))
}

const updateSQL = `
UPDATE forge_approval_requests
SET kind = $2, tenant = $3, requester = $4, reason = $5, status = $6,
    version = version + 1, payload = $7, decisions = $8, meta = $9,
    claimed_by = $10, created_at = $11, expires_at = $12, claimed_at = $13,
    decided_at = $14
WHERE id = $1 AND version = $15`

// Update persists r only when the stored version matches expect. Zero rows
// affected means either the id is gone or another writer moved the version;
// a follow-up existence check tells those apart, because the Manager
// retries one and gives up on the other.
func (s *Store) Update(ctx context.Context, r approval.Request, expect int64) error {
	decisions, meta, err := encodeState(r)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, updateSQL,
		r.ID, r.Kind, r.Tenant, r.Requester, r.Reason, int16(r.Status),
		[]byte(r.Payload), decisions, meta, r.ClaimedBy,
		r.CreatedAt, nullTime(r.ExpiresAt), nullTime(r.ClaimedAt), nullTime(r.DecidedAt),
		expect)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM forge_approval_requests WHERE id = $1)`, r.ID,
	).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return approval.ErrNotFound
	}
	return approval.ErrConflict
}

const listSQL = `
SELECT ` + cols + ` FROM forge_approval_requests
WHERE ($1 = '' OR tenant = $1)
  AND ($2 = '' OR kind = $2)
  AND ($3 = '' OR requester = $3)
  AND (cardinality($4::smallint[]) = 0 OR status = ANY($4))
  AND ($5::timestamptz IS NULL OR (expires_at IS NOT NULL AND expires_at < $5))
ORDER BY id DESC
LIMIT $6`

// List returns requests matching f, newest first (UUIDv7 ids are
// time-ordered, so id DESC is creation order).
func (s *Store) List(ctx context.Context, f approval.Filter) ([]approval.Request, error) {
	statuses := make([]int16, 0, len(f.Statuses))
	for _, st := range f.Statuses {
		statuses = append(statuses, int16(st))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	rows, err := s.pool.Query(ctx, listSQL,
		f.Tenant, f.Kind, f.Requester, statuses, nullTime(f.ExpiresBefore), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Non-nil empty on zero rows, matching the memory store.
	out := []approval.Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// defaultLimit caps an unbounded List so a large table cannot be pulled
// into memory by a zero-valued Filter.
const defaultLimit = 100

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanRequest(rw row) (approval.Request, error) {
	var (
		r         approval.Request
		status    int16
		payload   []byte
		decisions []byte
		exp       *time.Time
		claimed   *time.Time
		decided   *time.Time
	)
	err := rw.Scan(&r.ID, &r.Kind, &r.Tenant, &r.Requester, &r.Reason, &status,
		&r.Version, &payload, &decisions, &r.Meta, &r.ClaimedBy,
		&r.CreatedAt, &exp, &claimed, &decided)
	if errors.Is(err, pgx.ErrNoRows) {
		return approval.Request{}, approval.ErrNotFound
	}
	if err != nil {
		return approval.Request{}, err
	}
	r.Status = approval.Status(status)
	r.Payload = json.RawMessage(payload)
	if err := json.Unmarshal(decisions, &r.Decisions); err != nil {
		return approval.Request{}, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	if exp != nil {
		r.ExpiresAt = exp.UTC()
	}
	if claimed != nil {
		r.ClaimedAt = claimed.UTC()
	}
	if decided != nil {
		r.DecidedAt = decided.UTC()
	}
	return r, nil
}

// encodeState marshals the two JSON columns, normalizing nil to an empty
// array and an empty object so a reader never has to special-case null.
func encodeState(r approval.Request) (decisions []byte, meta map[string]string, err error) {
	d := r.Decisions
	if d == nil {
		d = []approval.Decision{}
	}
	decisions, err = json.Marshal(d)
	if err != nil {
		return nil, nil, err
	}
	meta = r.Meta
	if meta == nil {
		meta = map[string]string{}
	}
	return decisions, meta, nil
}

// nullTime maps a zero time to SQL NULL.
func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
```

Create `ops/approval/pgstore/doc.go` with a package comment mirroring `auth/apikey/pgstore/doc.go`'s shape: what it persists, that `Migrations` must be applied first under its own version table, and that the pool's lifecycle is the caller's.

- [ ] **Step 4: Run test to verify it passes**

Run: `just test-integration ./ops/approval/...`
Expected: PASS — the same conformance suite the memory store passes, now against Postgres, plus `TestConcurrentUpdatesConflict` reporting exactly one winner.

Run it twice in a row without dropping the database:
`just test-integration ./ops/approval/... && just test-integration ./ops/approval/...`
Expected: PASS both times. A failure on the second run means fixtures are not namespaced and the suite is reading a previous run's rows.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/
git commit -m "feat(approval): postgres store with CAS update and covering indexes"
```

---

## Task 13: Benchmarks and the optimization pass

**Files:**
- Create: `ops/approval/bench_test.go`
- Modify: whatever the profile proves hot (nothing, if nothing is)

**Interfaces:**
- Consumes: the whole public API.
- Produces: benchmark numbers for the PR body.

Benchmarks are required for every forge package, and any perf-motivated complexity requires a benchmark proving it. Measured wins only — do not "optimize" anything the numbers do not flag.

- [ ] **Step 1: Write the benchmarks**

Create `ops/approval/bench_test.go`:

```go
package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/approval"
)

func benchManager(b *testing.B, p approval.Policy, opts ...approval.Option) *approval.Manager {
	b.Helper()
	all := append([]approval.Option{
		approval.WithKind(kindPayout, p),
		approval.WithClock(clock.NewMock(fixedNow)),
	}, opts...)
	return approval.New(approval.NewMemoryStore(), all...)
}

func BenchmarkSubmit(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()
	payload := payoutPayload{PayoutID: "po_88", Amount: 250000}
	p := approval.SubmitParams{Requester: "alice", Reason: "invoice"}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := approval.Submit(ctx, m, kindPayout, payload, p); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkApprove(b *testing.B, quorum int) {
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		m := benchManager(b, approval.Policy{Quorum: quorum})
		r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
			approval.SubmitParams{Requester: "alice"})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := m.Approve(ctx, r.ID, actor("bob")); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApproveQuorum2(b *testing.B) { benchmarkApprove(b, 2) }
func BenchmarkApproveQuorum5(b *testing.B) { benchmarkApprove(b, 5) }

func BenchmarkApproveContended(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 100}, approval.WithMaxRetries(1000))
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			// Distinct approver ids; ErrAlreadyVoted/ErrNotPending are the
			// expected steady state once quorum is met, so ignore errors and
			// measure the CAS path itself.
			_, _ = m.Approve(ctx, r.ID, actor("approver-"+strconv.Itoa(i)))
		}
	})
}

func BenchmarkGet(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()
	r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
		approval.SubmitParams{Requester: "alice"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Get(ctx, r.ID); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkList100(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2, TTL: 24 * time.Hour})
	ctx := context.Background()
	for range 100 {
		if _, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
			approval.SubmitParams{Requester: "alice"}); err != nil {
			b.Fatal(err)
		}
	}
	f := approval.Filter{Statuses: []approval.Status{approval.Pending}}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.List(ctx, f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPayloadOf(b *testing.B) {
	m := benchManager(b, approval.Policy{Quorum: 2})
	r, err := approval.Submit(context.Background(), m, kindPayout,
		payoutPayload{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice"})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := approval.PayloadOf(kindPayout, r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkApproveWithDecider(b *testing.B) {
	allow := access.DeciderFunc(func(context.Context, access.Subject, access.Action, access.Resource) (access.Decision, error) {
		return access.Allow.Because("bench"), nil
	})
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		m := benchManager(b, approval.Policy{Quorum: 2}, approval.WithDecider(allow))
		r, err := approval.Submit(ctx, m, kindPayout, payoutPayload{},
			approval.SubmitParams{Requester: "alice"})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := m.Approve(ctx, r.ID, actor("bob")); err != nil {
			b.Fatal(err)
		}
	}
}
```

Add `"strconv"` to the imports.

- [ ] **Step 2: Capture the baseline**

Run: `just bench ./ops/approval/... | tee docs/superpowers/specs/2026-07-21-approval-bench-baseline.txt`
Expected: every benchmark reports ns/op and allocs/op.

- [ ] **Step 3: Optimize only what the numbers flag**

Read the baseline and look for these specific suspects, in order:

1. **`BenchmarkGet` allocating more than the store's clone requires.** `applyExpiry` must be allocation-free — it is pure timestamp comparison. If `Get` shows unexpected allocs, they are the memory store's `cloneRequest`, which is correct and required.
2. **`BenchmarkList100` allocating a second slice.** `List` filters in place with `kept := out[:0]`; if the profile shows a second backing array, the in-place filter was lost in a refactor.
3. **`BenchmarkApproveQuorum5` growing `Decisions` repeatedly.** `Submit` preallocates to `cap = Quorum`, so appends up to quorum must not reallocate. If they do, the preallocation is wrong.
4. **`BenchmarkPayloadOf`** is dominated by `encoding/json` — that is expected and not worth optimizing. Leave it.

If a fix is warranted, make it, rerun, and record the delta. If nothing is flagged, change nothing and say so in the PR. Do not add complexity for an unmeasured win.

Run: `just bench ./ops/approval/... | tee docs/superpowers/specs/2026-07-21-approval-bench-after.txt`

- [ ] **Step 4: Verify nothing regressed**

Run: `just test ./ops/approval/...`
Expected: PASS — optimization must not change behavior.

- [ ] **Step 5: Commit**

```bash
git add ops/approval/ docs/superpowers/specs/
git commit -m "test(approval): benchmarks and measured optimization pass"
```

---

## Task 14: Package documentation and catalog removal

**Files:**
- Create: `ops/approval/doc.go`
- Modify: `docs/packages.md` (delete the `ops/approval` entry)
- Test: `ops/approval/example_test.go`

**Interfaces:**
- Consumes: the whole public API.
- Produces: godoc and a compiling runnable example.

- [ ] **Step 1: Write the runnable example**

Create `ops/approval/example_test.go`. It must compile and its output must match, so it is a real test of the documented flow:

```go
package approval_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/ops/approval"
)

type releasePayout struct {
	PayoutID string `json:"payout_id"`
	Amount   int64  `json:"amount"`
}

var kindRelease = approval.NewKind[releasePayout]("payout.release")

func Example() {
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindRelease, approval.Policy{Quorum: 2}))
	ctx := context.Background()

	// The maker submits.
	req, err := approval.Submit(ctx, m, kindRelease,
		releasePayout{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "vendor invoice #4471"})
	if err != nil {
		panic(err)
	}
	fmt.Println("submitted:", req.Status)

	// The maker cannot be a checker.
	_, err = m.Approve(ctx, req.ID, approval.Actor{Subject: access.Subject{ID: "alice"}})
	fmt.Println("self-approval:", err)

	// Two checkers approve.
	got, _ := m.Approve(ctx, req.ID, approval.Actor{Subject: access.Subject{ID: "bob"}})
	fmt.Println("one of two:", got.Status)
	got, _ = m.Approve(ctx, req.ID, approval.Actor{Subject: access.Subject{ID: "carol"}})
	fmt.Println("two of two:", got.Status)

	// Exactly one executor runs the action.
	done, err := m.Execute(ctx, req.ID, "worker-1", func(ctx context.Context, r approval.Request) error {
		p, err := approval.PayloadOf(kindRelease, r)
		if err != nil {
			return err
		}
		fmt.Println("releasing:", p.PayoutID)
		return nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("finished:", done.Status)

	// Output:
	// submitted: pending
	// self-approval: approval: requester cannot decide own request
	// one of two: pending
	// two of two: approved
	// releasing: po_88
	// finished: executed
}
```

- [ ] **Step 2: Run the example to verify it fails**

Run: `just test ./ops/approval/...`
Expected: FAIL if any output line mismatches — fix the example or the code until it matches exactly.

- [ ] **Step 3: Write doc.go**

Create `ops/approval/doc.go`:

```go
// Package approval implements maker-checker dual control: a privileged
// action is recorded as a request, a second person approves or rejects it,
// and exactly one executor carries it out.
//
//	var KindReleasePayout = approval.NewKind[ReleasePayout]("payout.release")
//
//	m := approval.New(approval.NewMemoryStore(), // pgstore.New(pool) in production
//		approval.WithKind(KindReleasePayout, approval.Policy{Quorum: 2, TTL: 24 * time.Hour}))
//
//	req, err := approval.Submit(ctx, m, KindReleasePayout,
//		ReleasePayout{PayoutID: "po_88", Amount: amount},
//		approval.SubmitParams{Requester: "alice", Reason: "vendor invoice #4471"})
//
// Checkers vote with Approve and Reject. The quorum-th distinct approval
// moves the request to Approved; a single rejection is terminal.
//
//	req, err := m.Approve(ctx, req.ID, approval.Actor{
//		Subject: access.Subject{ID: "bob"}, Reason: "matched to invoice"})
//
// Three rules are structural and cannot be configured off: the requester
// may never decide their own request (at any quorum — Quorum 1 is still
// dual control), an approver counts once however many times they vote, and
// a rejected request never becomes approved.
//
// # Executing once
//
// Reaching Approved does not run anything. For actions where running twice
// is the real danger — releasing a payout, booking a calendar entry — claim
// the request first: Claim moves it to Executing atomically, so two
// operators or two racing webhook deliveries produce one winner and one
// ErrAlreadyClaimed.
//
//	done, err := m.Execute(ctx, req.ID, "worker-1", func(ctx context.Context, r approval.Request) error {
//		p, err := approval.PayloadOf(KindReleasePayout, r)
//		if err != nil {
//			return err
//		}
//		return payouts.Release(ctx, p.PayoutID, p.Amount)
//	})
//
// Execute wraps Claim, then Complete or Fail. Call them directly when the
// action spans processes. By default a claim never expires, so an executor
// that dies mid-action wedges the request until Release is called — the
// safe default for money. Policy.ClaimTTL opts into automatic takeover, and
// with it the action must be idempotent.
//
// Requests that nobody acts on expire after Policy.TTL. Expiry is derived
// on read, not swept: a Pending or Approved request past its deadline
// reports Expired and refuses every transition, with no background
// goroutine anywhere.
//
// # Eligibility, audit, and tenancy
//
// WithDecider gates who may vote through the auth/access decision seam,
// asking "<kind>:decide" with the request's kind, requester, and raw
// payload as attributes — enough for relational rules ("must be the
// requester's manager") and value-aware ones. It fails closed: a decider
// that errors refuses the vote.
//
// WithAuditor writes every transition to an ops/auditlog Recorder,
// including attempts the decider denied. WithScope confines every operation
// to a context-derived tenant, failing closed when none is available;
// single-tenant applications pay no ceremony.
//
//	m := approval.New(store,
//		approval.WithKind(KindReleasePayout, approval.Policy{Quorum: 2}),
//		approval.WithDecider(roles),
//		approval.WithAuditor(rec),
//		approval.WithScope(tenantFromContext))
//
// State lives behind the Store interface. NewMemoryStore is for tests and
// development; approval/pgstore persists to Postgres.
package approval
```

- [ ] **Step 4: Verify docs and remove the catalog entry**

Run: `just test ./ops/approval/... && go doc ./ops/approval | head -40`
Expected: PASS, and the package synopsis renders.

Delete the `**ops/approval**` entry from `docs/packages.md` — the catalog lists only unbuilt packages, and a shipped package's godoc is its reference. Then check for stale cross-references:

Run: `grep -n "ops/approval" docs/packages.md`
Expected: only the `auth/impersonation` dependency mention remains. Update its "(both planned)" note, since `ops/approval` is no longer planned.

- [ ] **Step 5: Full verification and commit**

Run: `just fmt ./ops/approval/... && just lint && just test ./ops/approval/...`
Expected: all clean. `nilaway` must be clean — it ignores `nolint` directives, so fix findings rather than suppressing them. If `nilaway` OOMs in CI, that is a known runner flake: rerun it.

```bash
git add ops/approval/ docs/packages.md
git commit -m "docs(approval): package documentation and catalog removal"
```

---

## Final verification

- [ ] `just fmt ./ops/approval/...` — clean
- [ ] `just lint` — clean, including the integration-tagged pass
- [ ] `just test ./ops/approval/...` — all green under `-race`
- [ ] `go test -race -count=20 -run "TestConcurrent" ./ops/approval/` — 20 consecutive passes
- [ ] `just test-integration ./ops/approval/...` — green with Docker
- [ ] `just bench ./ops/approval/...` — numbers captured for the PR body
- [ ] `docs/packages.md` no longer lists `ops/approval` as unbuilt
- [ ] PR body carries before/after benchmark numbers and notes which optimizations were measured wins (or that none were warranted)
