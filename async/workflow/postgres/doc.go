// Package pgworkflow is the Postgres workflow.Store: one row per run,
// checkpointed with optimistic locking on the version column so a worker
// whose queue lease was silently lost cannot regress a checkpoint the new
// owner already advanced. Apply Migrations with data/migration before use.
//
//	store, _ := pgworkflow.New(pool)
//	eng := workflow.NewEngine(broker, store)
//
// Terminal runs (completed, failed) stay behind as audit rows; schedule
// PurgeTerminalBefore (e.g. daily with a cutoff matching your retention
// policy) to keep the table bounded.
package pgworkflow
