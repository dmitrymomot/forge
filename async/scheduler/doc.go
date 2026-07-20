// Package scheduler turns cron and interval schedules into queue jobs: a
// supervisor.Service that enqueues a typed async/queue job whenever a
// schedule comes due. The scheduler only decides when to enqueue — handlers,
// retry, backoff, and dead-lettering stay in the queue worker, so scheduled
// work and ad-hoc work share one execution path.
//
// It runs on every instance and fires once per fleet: schedules are
// deterministic (every instance computes the same tick times), and the Store
// claim — a unique (name, scheduled_for) insert race — lets exactly one
// instance enqueue each tick. The in-memory store covers single-instance
// apps and tests; fleets share async/scheduler/postgres.
//
// # Usage
//
//	var KindDigest = queue.NewKind[DigestPayload]("email.digest")
//
//	client := queue.NewClient(broker)
//	sched, err := scheduler.New(client, scheduler.WithStore(store))
//	if err != nil { ... }
//	scheduler.Add(sched, "email.digest.daily", scheduler.MustCron("0 8 * * *"), KindDigest, DigestPayload{})
//	scheduler.AddFunc(sched, "report.hourly", scheduler.Every(time.Hour), KindReport,
//		func(scheduledFor time.Time) (ReportPayload, error) {
//			return ReportPayload{PeriodEnd: scheduledFor}, nil
//		})
//	// run sched under ops/supervisor next to the queue worker Service
//
// Cron specs are standard 5-field vixie expressions evaluated in UTC (CronIn
// for another zone); Every fires on epoch-aligned interval ticks. Both are
// Schedule implementations — bring a custom one for anything else.
//
// Missed ticks are not replayed: an instance that was down (or woke late)
// fires only the latest due tick per job, assuming a punctual instance
// claimed the older ones. A tick whose claim or enqueue fails is retried
// every Config.RetryInterval until it fires, another instance claims it, or
// the next tick supersedes it. Claims expire from the store after
// Config.Retention via a periodic sweep.
//
// Multi-tenant apps: scheduled jobs are system-initiated, so a
// scope-configured queue.Client would fail closed on the scheduler's
// background context. WithPushContext is the seam — inject the system tenant
// there so the client's scope hook resolves. Per-tenant schedules belong in a
// single fan-out job whose handler iterates tenants.
package scheduler
