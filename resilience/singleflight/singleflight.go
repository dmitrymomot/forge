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
	wg       sync.WaitGroup
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
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*call[V])
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, true, c.err
	}
	c := new(call[V])
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	func() {
		defer func() {
			if r := recover(); r != nil {
				c.panicVal = r
				c.err = fmt.Errorf("singleflight: panic in fn: %v", r)
			}
			c.wg.Done()
			g.mu.Lock()
			if g.m[key] == c {
				delete(g.m, key)
			}
			g.mu.Unlock()
		}()
		c.val, c.err = fn(context.WithoutCancel(ctx))
	}()
	if c.panicVal != nil {
		panic(c.panicVal) // leader re-panics; waiters already observed c.err
	}
	return c.val, false, c.err
}

// Forget drops any in-flight/last record for key so the next Do re-executes.
func (g *Group[V]) Forget(key string) {
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
}
