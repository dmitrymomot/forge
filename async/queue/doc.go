// Package queue is the durable background-work engine: producers Push typed
// jobs through a Client, a worker Service claims them from named queues and
// dispatches to registered handlers with retry, backoff, and dead-lettering.
//
// Storage is a pluggable, strictly-pull Broker: the built-in MemoryBroker
// (tests, single-process apps), async/queue/postgres (SKIP LOCKED claiming,
// transactional enqueue), and async/queue/redis (Streams + consumer groups).
// The engine — not the driver — owns retry, delay, and dead-letter semantics,
// so behavior is identical across backends.
//
// # Delivery contract
//
// Delivery is at-least-once via claim-with-lease: a claimed job is invisible
// for the lease duration and the Service heartbeats the lease while the
// handler runs; if the process crashes the lease expires and the job is
// redelivered. Claims carry a fencing token: a worker whose lease was lost
// cannot ack, retry, kill, or extend the job anymore (queue.ErrLeaseLost), so
// duplicate execution is confined to true crash-mid-handler redelivery.
// HANDLERS MUST STILL BE IDEMPOTENT. Ordering is not guaranteed.
//
// # Usage
//
//	var KindSendWelcome = queue.NewKind[SendWelcome]("email.send_welcome")
//
//	svc, _ := queue.NewService(broker,
//		queue.WithQueues(map[string]int{"critical": 6, "default": 3, "low": 1}),
//	)
//	queue.Register(svc, KindSendWelcome, func(ctx context.Context, p SendWelcome) error {
//		return mailer.Send(ctx, p.Email)
//	})
//	// run under ops/supervisor: supervisor.WithService(svc)
//
//	client := queue.NewClient(broker)
//	err := queue.Push(ctx, client, KindSendWelcome, SendWelcome{Email: "a@b.c"},
//		queue.WithQueue("critical"), queue.WithDelay(time.Minute))
//	err = queue.PushMany(ctx, client, KindSendWelcome, batch)      // bulk enqueue, one round trip
//
// Handler verdicts: return nil to complete, queue.SkipRetry(err) to
// dead-letter immediately (poison input), queue.Cancel to discard a moot job;
// any other error retries with backoff until the attempt budget is spent,
// then dead-letters. Inspect and recover via Client.ListDead, Client.Requeue,
// Client.Purge; feed ops/health from Client.Stats.
//
// Every handler runs under Config.HandlerTimeout (default 10m) unless its
// kind sets queue.WithHandlerTimeout — queue.WithHandlerTimeout(0) opts a
// long-running kind out. Dead-lettered jobs are purged after
// Config.DeadRetention (default 30 days; 0 keeps them forever) by a sweep
// every worker instance runs; the sweep also drives optional broker
// housekeeping (queue.Maintainer).
//
// Multi-tenant apps configure queue.WithScope on the Client (captures the
// tenant into the job, fail-closed) and queue.WithScopeContext on the Service
// (restores it into the handler context). Single-tenant apps configure
// neither.
//
// The queue is the unit of routing: every kind pushed to a queue must be
// registered on every Service draining that queue; to split kinds across
// worker deployments, split the queues.
package queue
