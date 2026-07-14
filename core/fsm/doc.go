// Package fsm provides an immutable, compiled finite state machine:
// declared states and target-driven transitions (edges from -> to) with
// guards, hooks, and illegal-transition errors. A Machine is a stateless
// table — the current state lives in the caller's storage (a status
// column) and is passed into every call — so one compiled machine is
// safely shared across goroutines. Persistence is caller-owned: Fire
// approves or denies a transition and enriches the entity; the caller
// writes the status column afterwards.
//
// Guards are side-effect-free checks over preloaded data on the subject
// V; keep I/O out of guards so ErrGuardDenied always means a domain
// denial. Hooks may mutate V and run only after every guard passed; an
// error aborts the transition. Everything inside Fire happens before
// persistence — post-persist effects (notifications, outbox events) run
// after the caller's own DB write. Fire order: exit-guards(from),
// edge-guards, enter-guards(to), then exit-hooks(from), edge-hooks,
// enter-hooks(to); first error aborts.
//
// # Compile-time flows
//
// Declare a typed lifecycle once at package init. The Define anchor binds
// both type parameters so every part infers them:
//
//	type Status string
//
//	const (
//		StatusDraft  Status = "draft"
//		StatusIssued Status = "issued"
//		StatusPaid   Status = "paid"
//		StatusVoid   Status = "void"
//	)
//
//	var d fsm.Define[Status, *Invoice]
//
//	var InvoiceFSM = fsm.MustNew(StatusDraft,
//		d.Edge(StatusDraft, StatusIssued, d.Hook(assignNumber)),
//		d.Edge(StatusIssued, StatusPaid),
//		d.EdgeFromAny(StatusVoid, d.Guard(notPaid)),
//		d.OnEnter(StatusIssued, d.Hook(stampIssuedAt)),
//	)
//
//	func Transition(ctx context.Context, inv *Invoice, to Status) error {
//		if err := InvoiceFSM.Fire(ctx, inv, inv.Status, to); err != nil {
//			return err
//		}
//		inv.Status = to
//		return saveInvoice(ctx, inv) // conditional write, see below
//	}
//
// # Tenant-defined flows
//
// Runtime flows load a Definition (JSON) from the consumer's DB and
// compile it against the app's registered guard/hook vocabulary. Compile
// fails closed with every issue in one single-line error — validate at
// flow-save time and a stored definition always fires cleanly. Cache
// compiled machines by (tenant, flow, version); they are immutable, so a
// version-keyed cache never needs invalidation:
//
//	reg := fsm.Registry[*Move]{
//		Guards: map[string]fsm.Func[string, *Move]{
//			"admin_only":      adminOnly,
//			"subtasks_closed": subtasksClosed,
//		},
//		Hooks: map[string]fsm.Func[string, *Move]{
//			"stamp_completed": stampCompleted,
//		},
//	}
//
//	var def fsm.Definition
//	if err := json.Unmarshal(row.FlowJSON, &def); err != nil {
//		return err
//	}
//	m, err := fsm.Compile(def, reg) // cache under row.ProjectID + ":" + row.Version
//
//	err = m.Fire(ctx, &Move{Task: task, Actor: actor}, task.Status, target)
//	switch {
//	case errors.Is(err, fsm.ErrGuardDenied):
//		// 422: err carries the human reason ("subtasks still open")
//	case errors.Is(err, fsm.ErrIllegalTransition), errors.Is(err, fsm.ErrUnknownState):
//		// 409: stale board — flow or task changed under the user
//	case err != nil:
//		// 500: a hook broke; nothing was persisted
//	}
//
// Allowed returns the targets the current entity may actually move to
// (guards evaluated, hooks never run) — it renders per-user status
// dropdowns: an edge that exists but is guarded ("done -> open" for
// admins only) appears only for entities that pass.
//
// # Consumer patterns
//
// Persist with a conditional write — UPDATE ... SET status = $to WHERE id
// = $id AND status = $from — and treat 0 affected rows as a stale-board
// conflict: Fire validated the from the caller loaded, and a concurrent
// writer may have moved the row since. Timer-driven transitions ("solved
// -> closed after 72h", bonus expiry) are a consumer cron/jobqueue sweep
// that selects due rows and calls Fire with a system actor. Definitions
// arrive from the consumer's own flow-save API; size limits belong there.
//
// Out of scope forever: timers, auto/cascade transitions (a hook cannot
// fire another transition), per-instance state or persistence, flow
// versioning/migration, assignment/roles/SLA semantics, definition
// storage, post-persist effect execution, and parallel/composite states —
// an entity with two independent lifecycles has two status columns, one
// machine each.
package fsm
