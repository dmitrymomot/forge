// Package lockout counts authentication failures per identity and escalates
// lockout windows: after a configurable number of free failures, each further
// failure locks the identity for base × factor^k, capped at a maximum
// (factor 1.0 gives fixed-duration locks). It is not rate shaping (that is
// resilience/ratelimit) and not cumulative caps (resilience/quota) —
// failure-triggered escalation only.
//
// Failure counts ride the ratelimit counter seam; lock markers ride the
// cache TTL-KV seam. Both bring their own backends, so no drivers ship here:
//
//	counters := ratelimit.NewMemoryStore() // or ratelimit/redisstore, ratelimit/pgstore
//	locks := cache.NewMemoryStore()        // or cache/redis
//	lk, err := lockout.New(counters, locks,
//		lockout.WithThreshold(5),
//		lockout.WithBaseLock(time.Minute),
//		lockout.WithMaxLock(15*time.Minute),
//	)
//
// The explicit core wires into any login or OTP flow:
//
//	res, err := lk.Allow(ctx, email)
//	if err != nil { /* 500 */ }
//	if res.Locked { /* 429 + Retry-After: res.RetryAfter */ }
//
//	// wrong credentials:
//	res, err = lk.Fail(ctx, email) // res.Locked → "locked for res.RetryAfter"
//
//	// success:
//	err = lk.Reset(ctx, email)
//
// Do wraps the same cycle around a callback; only errors matching
// ErrFailedAttempt count, so infrastructure failures never lock a user out:
//
//	err := lk.Do(ctx, email, func(ctx context.Context) error {
//		if !credentialsValid(ctx) {
//			return lockout.ErrFailedAttempt
//		}
//		return nil
//	})
//
// Middleware gates the Allow half over net/http and stashes the extracted
// key in the context for the handler's Fail/Reset calls:
//
//	mw := lk.Middleware(func(r *http.Request) string { return r.PostFormValue("email") })
//	mux.Handle("POST /login", mw(loginHandler))
//
// It fails closed (503) on store errors by default; WithFailOpen restores
// availability-first behavior.
//
// Caller keys (emails, phones, IPs) are SHA-256 hashed into store keys —
// PII hygiene, not secrecy. Multi-tenant apps set WithScope to derive a
// tenant scope from the context on every call; an error or empty scope fails
// closed with ErrScope. Single-tenant apps omit it and pay zero ceremony.
//
// The failure counter's TTL is fixed when the first failure of a burst
// creates it (the counter seam never extends a live TTL), so keep the window
// at or above the maximum lock — the defaults (30m window, 15m max lock)
// comply — or escalation memory expires before the last lock does.
//
// Out of scope: CAPTCHA hooks, lockout notifications, IP reputation, and
// admin unlock APIs beyond Reset (which is the unlock). Successful-login
// anomaly detection belongs to web/fingerprint.
package lockout
