package lockout

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/resilience/ratelimit"
)

// Result reports the lockout state of one identity.
type Result struct {
	Until      time.Time     // lock expiry; zero when unlocked
	RetryAfter time.Duration // >0 when Locked
	Failures   int64         // failures recorded in the current memory window
	Remaining  int64         // free attempts left before the next lock
	Locked     bool
}

// Locker tracks authentication failures per identity and escalates lockout
// windows. Failure counts ride the ratelimit counter seam; lock markers ride
// the cache TTL-KV seam. Store lifecycles remain the caller's.
type Locker struct {
	counters ratelimit.Store
	locks    cache.Store
	cfg      config
}

// New builds a Locker over the two stores. See the With* options for the
// escalation defaults (5 free failures, then 1m locks doubling up to 15m,
// failures remembered for 30m).
func New(counters ratelimit.Store, locks cache.Store, opts ...Option) (*Locker, error) {
	cfg := config{
		threshold: 5,
		baseLock:  time.Minute,
		factor:    2.0,
		maxLock:   15 * time.Minute,
		window:    30 * time.Minute,
		clk:       clock.System(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	switch {
	case counters == nil:
		return nil, errors.New("lockout: nil counter store")
	case locks == nil:
		return nil, errors.New("lockout: nil lock store")
	case cfg.threshold < 1:
		return nil, errors.New("lockout: threshold must be >= 1")
	case cfg.baseLock <= 0:
		return nil, errors.New("lockout: base lock must be > 0")
	case cfg.factor < 1 || math.IsNaN(cfg.factor):
		return nil, errors.New("lockout: factor must be >= 1")
	case cfg.maxLock < cfg.baseLock:
		return nil, errors.New("lockout: max lock must be >= base lock")
	case cfg.window <= 0:
		return nil, errors.New("lockout: window must be > 0")
	}
	return &Locker{counters: counters, locks: locks, cfg: cfg}, nil
}

// Allow reports whether key may attempt authentication. It is a read-only
// pre-check: a parallel burst may pass Allow before a lock lands, but the
// lock still lands exactly once (see Fail). On the locked path Failures and
// Remaining stay zero — no second store round-trip.
func (l *Locker) Allow(ctx context.Context, key string) (Result, error) {
	failsKey, lockKey, err := l.keys(ctx, key)
	if err != nil {
		return Result{}, err
	}
	res, locked, err := l.lockState(ctx, lockKey)
	if err != nil {
		return Result{}, err
	}
	if locked {
		return res, nil
	}
	n, err := l.counters.Get(ctx, failsKey)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
	return Result{Failures: n, Remaining: max(l.cfg.threshold-n, 0)}, nil
}

// Fail records one authentication failure for key. Crossing the threshold
// creates a lock whose duration escalates with the failure count; exactly one
// concurrent crosser creates the marker (SetNX), losers report the winner's
// expiry. A Fail while already locked increments the counter (escalating the
// next lock) but never extends the current one. Below the threshold, the
// returned Locked reflects only the failure counter, not the lock marker —
// Allow is the authoritative check, since a live lock marker can outlast the
// failure-counter window and make a below-threshold Fail report Locked:false
// while Allow reports locked.
func (l *Locker) Fail(ctx context.Context, key string) (Result, error) {
	failsKey, lockKey, err := l.keys(ctx, key)
	if err != nil {
		return Result{}, err
	}
	n, err := l.counters.Incr(ctx, failsKey, 1, l.cfg.window)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
	if n <= l.cfg.threshold {
		return Result{Failures: n, Remaining: l.cfg.threshold - n}, nil
	}

	dur := l.lockDuration(n)
	until := l.cfg.clk.Now().Add(dur)
	val := strconv.AppendInt(nil, until.UnixMilli(), 10)
	err = l.locks.Set(ctx, lockKey, val, cache.WithTTL(dur), cache.WithSetNonExist())
	switch {
	case err == nil:
		return Result{Locked: true, RetryAfter: dur, Until: until, Failures: n}, nil
	case errors.Is(err, cache.ErrExists):
		res, locked, err := l.lockState(ctx, lockKey)
		if err != nil {
			return Result{}, err
		}
		if !locked {
			// The concurrent winner's marker expired between SetNX and Get:
			// report unlocked; the next failure locks again.
			return Result{Failures: n}, nil
		}
		res.Failures = n
		return res, nil
	default:
		return Result{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
}

// Reset clears the failure count and any active lock for key. Call it after
// successful authentication.
func (l *Locker) Reset(ctx context.Context, key string) error {
	failsKey, lockKey, err := l.keys(ctx, key)
	if err != nil {
		return err
	}
	if err := errors.Join(l.counters.Reset(ctx, failsKey), l.locks.Delete(ctx, lockKey)); err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	return nil
}

// lockState reads the lock marker. A marker whose embedded expiry has passed
// is treated as unlocked even when its store TTL has not fired yet (clock
// skew across nodes).
func (l *Locker) lockState(ctx context.Context, lockKey string) (Result, bool, error) {
	raw, err := l.locks.Get(ctx, lockKey)
	if errors.Is(err, cache.ErrNotFound) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("%w: %w", ErrStore, err)
	}
	ms, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return Result{}, false, fmt.Errorf("%w: malformed lock marker: %w", ErrStore, err)
	}
	until := time.UnixMilli(ms)
	now := l.cfg.clk.Now()
	if !until.After(now) {
		return Result{}, false, nil
	}
	return Result{Locked: true, RetryAfter: until.Sub(now), Until: until}, true, nil
}

// lockDuration computes min(base × factor^(n-threshold-1), maxLock), clamping
// before the float→Duration conversion so huge counts cannot overflow.
func (l *Locker) lockDuration(n int64) time.Duration {
	exp := float64(n - l.cfg.threshold - 1)
	d := float64(l.cfg.baseLock) * math.Pow(l.cfg.factor, exp)
	if d >= float64(l.cfg.maxLock) || math.IsInf(d, 1) {
		return l.cfg.maxLock
	}
	return time.Duration(d)
}
