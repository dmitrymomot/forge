// Package quota caps cumulative usage per subject against a caller-owned limit —
// the plan-entitlement counterpart to ratelimit. It rides the shared
// ratelimit.Store counter seam and covers three shapes behind one Meter:
// calendar-window meters (events/month), rolling-window meters (fixed-window
// approximation), and gauges (live seats/storage, no reset).
//
// Feature-tier entitlement ("feature X on tier Y") is NOT a quota concern — that
// is set-membership; use ops/featureflag.
//
// # Usage
//
//	store := ratelimit.NewMemoryStore() // or a durable Store of your own
//	m := quota.New(store, quota.Calendar(quota.Monthly, nil))
//	lim := quota.Limit{Included: 10_000, Max: 12_000} // 10k included, 2k overage
//	res, err := m.Allow(ctx, tenantID, tokens, lim)
//	if err != nil { /* ... */ }
//	if !res.Allowed { return errPlanExceeded }
//	if res.Overage > 0 { billing.RecordOverage(tenantID, res.Overage) }
//
// Gauges need a durable store: the in-process memory store never expires a
// no-expiry key but loses it on restart, so back seats and storage caps with
// a durable Store implementation.
//
// # Concurrency and durability
//
// Allow's incr-then-rollback is not atomic: under concurrency a subject can be
// transiently over-counted, and a request can even be wrongly rejected if it
// interleaves with another caller's not-yet-reverted excess. Treat Allow as
// best-effort under high contention, not an exact gate. For hard correctness
// use a backend with its own transactional guarantees or accept the small
// transient overshoot.
//
// If the compensating rollback increment itself fails (a flaky Store backend),
// Allow returns that error and the forward increment stays applied — quota is
// burned on a rejected call. The in-memory store never errors; this matters
// only for remote backends.
package quota
