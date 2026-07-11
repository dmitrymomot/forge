// Package lock is a distributed mutex with TTL leases, monotonic fencing
// tokens, and auto-refresh, plus cluster-singleton leader election. It has an
// in-process memory store; real backends live in lock/pgstore (table lease) and
// lock/redisstore (single-instance).
//
// # Usage
//
//	l := lock.New(store, lock.WithTTL(30*time.Second))
//
//	// mutual exclusion around a critical section:
//	lease, err := l.Acquire(ctx, "tenant:42:import")
//	if err != nil { return err }
//	defer lease.Release(ctx)
//	importBatch(ctx, lease.Fence()) // pass the fence to reject stale holders
//
//	// cluster singleton — run on exactly one node, failover automatic:
//	supervisor.Run(ctx, supervisor.WithService(l.RunOnLeader("outbox", "outbox-pump", outbox.Run)))
package lock
