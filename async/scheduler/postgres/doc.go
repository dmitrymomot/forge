// Package pgscheduler is the Postgres scheduler.Store: a fleet-shared claim
// table whose unique (name, scheduled_for) primary key turns concurrent
// instances firing the same tick into an insert race exactly one wins. Apply
// Migrations with data/migration before use.
//
//	store, err := pgscheduler.NewStore(pool)
//	if err != nil { ... }
//	sched, err := scheduler.New(client, scheduler.WithStore(store))
//
// Claims only dedupe ticks a lagging instance could still fire, so rows need
// to outlive fleet clock spread, not live forever: the scheduler's own sweep
// (Config.Retention, Config.SweepInterval) keeps the table bounded.
//
// Postgres stores timestamps at microsecond precision: distinct ticks less
// than 1µs apart would collide on one claim row, so keep Every intervals at
// microsecond granularity or coarser (Cron ticks are whole minutes).
package pgscheduler
