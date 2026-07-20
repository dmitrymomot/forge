// Package workflow runs DB-checkpointed linear step sequences over the queue
// engine: a typed state value flows through named steps in order, the run is
// checkpointed to a Store after every step, and a crashed or restarted worker
// resumes from the last checkpoint instead of starting over. On permanent
// failure, completed steps' compensations run in reverse order (a payout
// pipeline that must undo its ledger debit). No DAG, no DSL, no timers — not a
// Temporal clone.
//
// # Usage
//
//	type Onboard struct {
//		UserID    string
//		AccountID string
//	}
//
//	var onboard = workflow.New("user.onboard",
//		workflow.Step[Onboard]{Name: "create_account", Run: createAccount, Compensate: deleteAccount},
//		workflow.Step[Onboard]{Name: "provision", Run: provision, Compensate: deprovision},
//		workflow.Step[Onboard]{Name: "send_welcome", Run: sendWelcome},
//	)
//
//	eng := workflow.NewEngine(broker, store)   // store: NewMemoryStore() or async/workflow/postgres
//	workflow.Register(eng, onboard)
//	svc, _ := workflow.NewService(eng)         // run under ops/supervisor
//	runID, _ := workflow.Start(ctx, eng, onboard, Onboard{UserID: "u1"})
//
// Each workflow drains its own queue (named after the workflow), so a slow
// workflow only delays itself. One handler invocation drives as many steps as
// it can; every step boundary is a checkpoint, so the engine's at-least-once
// redelivery — after a crash, a lost lease, or a retry — resumes at the first
// incomplete step. A step can therefore run more than once: STEPS MUST BE
// IDEMPOTENT, exactly like queue handlers.
//
// # Failure semantics
//
// A step error is transient by default: the run checkpoints the failed
// attempt and the job retries with the worker's backoff, re-entering at the
// same step. A handler-timeout expiry mid-step counts as a transient failure
// of that step (a chronically slow step eventually spends its budget); a
// lost queue lease does not — the new claim owns the run. A step fails
// permanently when it returns workflow.Fail(err) (business failure — no
// retry can help) or when its attempt budget (Step.MaxAttempts, default
// WithStepAttempts) is spent. Permanent failure
// flips the run to compensating: completed steps' Compensate funcs run newest
// first, with the same checkpoint, retry, and budget rules; steps without a
// Compensate are skipped. When compensation finishes — or no completed step
// has one — the run ends failed with the original error recorded on
// Run.Error. A compensation that exhausts its own budget dead-letters the
// driving job with the run left compensating; queue.Client.Requeue resumes
// compensation from the checkpoint. The run row in the Store is the source of
// truth; inspect it (or the engine's log) for outcomes.
//
// The driving job's attempt budget is derived from the sum of all step and
// compensation budgets plus a margin for infrastructure hiccups, so the queue
// engine never dead-letters a run that is still making progress. If a run
// does dead-letter (e.g. the Store was down for a long stretch), the
// checkpoint is intact and queue.Client.Requeue resumes it.
//
// Multi-tenant apps configure workflow.WithScope on the engine (captures the
// tenant into the run and its job, fail-closed) and pass
// queue.WithScopeContext to NewService (restores it into step context).
// Single-tenant apps configure neither.
package workflow
