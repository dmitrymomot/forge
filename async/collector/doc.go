// Package collector is write-behind ingestion for proven-hot fire-and-forget
// paths — click streams, beacons, telemetry. Add buffers an event into a
// bounded in-memory buffer and returns immediately, never blocking the
// request path; a single flusher goroutine delivers batches to a Sink when
// they reach Config.BatchSize or Config.FlushInterval elapses, whichever
// comes first.
//
// The overload policy is explicit: when the buffer is full, Add drops the new
// event (drop-newest), counts it, and returns ErrBufferFull; the flusher logs
// drop deltas once per interval instead of per event. A Sink error loses that
// batch — counted in Stats.Lost, logged, never retried; wrap the sink with
// resilience/retry when a transient backend warrants attempts. Loss is the
// contract: reach for async/outbox or async/eventbus first, and use this
// package only when per-event publish provably shows up in a profile. No
// dedup — double-fires and unique-key rules belong to the downstream
// pipeline.
//
// # Usage
//
//	sink := collector.SinkFunc[Click](func(ctx context.Context, batch []Click) error {
//		return warehouse.InsertClicks(ctx, batch)
//	})
//	c, err := collector.New(sink, collector.WithLogger(log))
//	if err != nil {
//		panic(err)
//	}
//
//	// In the request path: never blocks, error is optional to observe.
//	_ = c.Add(r.Context(), Click{Path: r.URL.Path})
//
//	// Under ops/supervisor: cancellation drains the buffer through the sink.
//	err = supervisor.Run(ctx, supervisor.WithService(c))
//
// A Collector is a supervisor.Service: Run flushes until ctx is cancelled,
// then stops accepting (Add returns ErrClosed), drains everything already
// buffered, and returns. Events added while the collector is not running
// simply accumulate up to BufferSize.
//
// Multi-tenant apps configure WithScope (captures the tenant on every Add,
// fail-closed: a hook error or empty scope fails Add with ErrScopeMissing)
// and WithScopeContext (flushes partition by captured scope and the hook
// restores each scope into its Flush context). Single-tenant apps configure
// neither and the sink receives whole batches with a plain context.
package collector
