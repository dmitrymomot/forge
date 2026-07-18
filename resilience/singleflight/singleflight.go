package singleflight

import (
	"context"
	"fmt"
	"sync"
)

type call[V any] struct {
	val      V
	err      error
	panicVal any
	done     chan struct{}
}

// Group deduplicates concurrent Do calls by key. The zero value is ready to
// use; a Group must not be copied after first use.
type Group[V any] struct {
	m  map[string]*call[V]
	mu sync.Mutex
}

// Do runs fn once for key while a call is in flight, sharing the result with
// concurrent callers. shared reports whether the result came from another
// caller's execution. fn runs under a cancellation-detached copy of ctx so one
// caller cancelling does not abort the shared work; each caller's own ctx still
// bounds its wait via fn's returned error, not the wait itself.
func (g *Group[V]) Do(ctx context.Context, key string, fn func(context.Context) (V, error)) (V, bool, error) {
	c, shared := g.join(key)
	if shared {
		<-c.done
		return c.val, true, c.err
	}

	g.run(context.WithoutCancel(ctx), key, c, fn)
	if c.panicVal != nil {
		panic(c.panicVal) // leader re-panics; waiters already observed c.err
	}
	return c.val, false, c.err
}

// DoDetached is Do with context-bounded waits: fn runs once per key in its
// own goroutine under a cancellation-detached copy of the initiating caller's
// ctx, and every caller — initiator and joiners alike — waits only until its
// own ctx ends, receiving ctx.Err() while the shared execution keeps running
// for the callers that stay. Use it when fn may outlive a request deadline
// (a slow database read behind a redirect hot path) and no single caller may
// be pinned past its own deadline. A panic in fn is converted to an error
// every waiter observes; there is no caller to re-panic into. Do and
// DoDetached share the per-key flight, so mixed callers coalesce too.
func (g *Group[V]) DoDetached(ctx context.Context, key string, fn func(context.Context) (V, error)) (V, bool, error) {
	c, shared := g.join(key)
	if !shared {
		go g.run(context.WithoutCancel(ctx), key, c, fn)
	}

	select {
	case <-c.done:
		return c.val, shared, c.err
	case <-ctx.Done():
		var zero V
		return zero, shared, ctx.Err()
	}
}

// join returns key's in-flight call, or registers a fresh one when none
// exists. shared reports whether the caller joined an existing flight rather
// than leading a new one; the leader is responsible for running it.
func (g *Group[V]) join(key string) (*call[V], bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call[V])
	}
	// The c != nil arm is unreachable (only non-nil calls are registered) but
	// lets nilaway prove join's result needs no guarding at the call sites.
	if c, ok := g.m[key]; ok && c != nil {
		g.mu.Unlock()
		return c, true
	}
	c := &call[V]{done: make(chan struct{})}
	g.m[key] = c
	g.mu.Unlock()
	return c, false
}

// run executes fn into c and closes c.done exactly once, converting a panic
// to an error and deregistering the flight so the next call for key starts
// fresh.
func (g *Group[V]) run(ctx context.Context, key string, c *call[V], fn func(context.Context) (V, error)) {
	defer func() {
		if r := recover(); r != nil {
			c.panicVal = r
			c.err = fmt.Errorf("singleflight: panic in fn: %v", r)
		}
		close(c.done)
		g.mu.Lock()
		if g.m[key] == c {
			delete(g.m, key)
		}
		g.mu.Unlock()
	}()
	c.val, c.err = fn(ctx)
}

// Forget drops the in-flight call for key, if any, so the next Do for that
// key starts a fresh execution instead of joining a stale one.
func (g *Group[V]) Forget(key string) {
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
}
