package featureflag_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/featureflag"
)

// countingProvider counts underlying calls; optionally errors, optionally blocks.
type countingProvider struct {
	mu    sync.Mutex
	flags map[string]featureflag.Flags // scope → flags
	err   error
	calls atomic.Int64
	gate  chan struct{} // when non-nil, Flag blocks until closed
}

var scopeCtx = ctxkey.New[string]("test.scope")

func (p *countingProvider) Flag(ctx context.Context, key string) (featureflag.Flag, bool, error) {
	p.calls.Add(1)
	if p.gate != nil {
		<-p.gate
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return featureflag.Flag{}, false, p.err
	}
	scope, _ := scopeCtx.From(ctx)
	f, ok := p.flags[scope][key]
	return f, ok, nil
}

func scopeOf(ctx context.Context) string { s, _ := scopeCtx.From(ctx); return s }

func TestCached(t *testing.T) {
	t.Parallel()
	enabled := func(v string) featureflag.Flag {
		return featureflag.Flag{Value: v, Enabled: true, Rollout: 100}
	}

	t.Run("hit within TTL skips provider", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		for range 10 {
			f, ok, err := c.Flag(t.Context(), "f")
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, "x", f.Value)
		}
		assert.EqualValues(t, 1, p.calls.Load())
	})

	t.Run("TTL expiry refreshes", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		_, _, _ = c.Flag(t.Context(), "f")
		p.mu.Lock()
		p.flags[""]["f"] = enabled("y")
		p.mu.Unlock()
		mock.Advance(31 * time.Second)
		f, _, _ := c.Flag(t.Context(), "f")
		assert.Equal(t, "y", f.Value)
		assert.EqualValues(t, 2, p.calls.Load())
	})

	t.Run("misses are cached", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		for range 5 {
			_, ok, err := c.Flag(t.Context(), "missing")
			require.NoError(t, err)
			assert.False(t, ok)
		}
		assert.EqualValues(t, 1, p.calls.Load())
	})

	t.Run("serve stale on refresh error", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}}
		mock := clock.NewMock(time.Unix(1000, 0))
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheClock(mock))
		_, _, _ = c.Flag(t.Context(), "f")
		p.mu.Lock()
		p.err = assert.AnError
		p.mu.Unlock()
		mock.Advance(31 * time.Second)
		f, ok, err := c.Flag(t.Context(), "f")
		require.NoError(t, err, "stale value served, error swallowed")
		require.True(t, ok)
		assert.Equal(t, "x", f.Value)
	})

	t.Run("cold error propagates", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{err: assert.AnError}
		c := featureflag.Cached(p, 30*time.Second)
		_, _, err := c.Flag(t.Context(), "f")
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("scope isolation", func(t *testing.T) {
		t.Parallel()
		p := &countingProvider{flags: map[string]featureflag.Flags{
			"tenant_a": {"f": enabled("a")},
			"tenant_b": {"f": enabled("b")},
		}}
		c := featureflag.Cached(p, 30*time.Second, featureflag.CacheKey(scopeOf))
		ctxA := scopeCtx.With(t.Context(), "tenant_a")
		ctxB := scopeCtx.With(t.Context(), "tenant_b")
		fa, _, _ := c.Flag(ctxA, "f")
		fb, _, _ := c.Flag(ctxB, "f")
		assert.Equal(t, "a", fa.Value)
		assert.Equal(t, "b", fb.Value)
		// both cached independently
		_, _, _ = c.Flag(ctxA, "f")
		_, _, _ = c.Flag(ctxB, "f")
		assert.EqualValues(t, 2, p.calls.Load())
	})

	t.Run("singleflight collapses concurrent misses", func(t *testing.T) {
		t.Parallel()
		gate := make(chan struct{})
		p := &countingProvider{flags: map[string]featureflag.Flags{"": {"f": enabled("x")}}, gate: gate}
		c := featureflag.Cached(p, 30*time.Second)
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				_, _, _ = c.Flag(context.Background(), "f")
			})
		}
		time.Sleep(50 * time.Millisecond) // let goroutines pile onto the flight
		close(gate)
		wg.Wait()
		assert.EqualValues(t, 1, p.calls.Load(), "one provider call for 20 concurrent readers")
	})

	t.Run("lister passthrough", func(t *testing.T) {
		t.Parallel()
		mem := featureflag.NewMemory(featureflag.Flags{"f": enabled("x")})
		c := featureflag.Cached(mem, time.Second)
		l, ok := c.(featureflag.Lister)
		require.True(t, ok, "Cached over a Lister must expose All")
		all, err := l.All(t.Context())
		require.NoError(t, err)
		assert.Len(t, all, 1)

		plain := &countingProvider{flags: map[string]featureflag.Flags{"": {}}}
		_, isLister := featureflag.Cached(plain, time.Second).(featureflag.Lister)
		assert.False(t, isLister, "Cached over a non-Lister must not fake All")
	})

	t.Run("nil provider panics with sentinel error", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			require.NotNil(t, r)
			err, ok := r.(error)
			require.True(t, ok, "panic value must be an error")
			assert.ErrorIs(t, err, featureflag.ErrNilProvider)
		}()
		featureflag.Cached(nil, time.Second)
	})
}
