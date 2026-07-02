// Package parallel runs work concurrently with a bounded worker count. By
// default it is fail-fast (first error cancels the rest); WithCollectAll runs
// everything and joins all errors.
package parallel

import (
	"context"
	"errors"
	"sync"
)

type config struct {
	limit      int
	collectAll bool
}

// Option configures a Group.
type Option func(*config)

// WithLimit bounds concurrent goroutines. n ≤ 0 means unbounded.
func WithLimit(n int) Option { return func(c *config) { c.limit = n } }

// WithCollectAll runs every task to completion and joins all errors instead of
// cancelling siblings on the first failure.
func WithCollectAll() Option { return func(c *config) { c.collectAll = true } }

// Group runs a set of functions concurrently. Construct it with New.
type Group struct {
	ctx        context.Context
	firstErr   error
	cancel     context.CancelFunc
	sem        chan struct{}
	errs       []error
	wg         sync.WaitGroup
	once       sync.Once
	mu         sync.Mutex
	collectAll bool
}

// New returns a Group and a derived context. In fail-fast mode the context is
// cancelled on the first error; it is always cancelled once Wait returns.
func New(ctx context.Context, opts ...Option) (*Group, context.Context) {
	c := config{}
	for _, o := range opts {
		o(&c)
	}
	ctx, cancel := context.WithCancel(ctx)
	g := &Group{ctx: ctx, cancel: cancel, collectAll: c.collectAll}
	if c.limit > 0 {
		g.sem = make(chan struct{}, c.limit)
	}
	return g, ctx
}

// Go runs fn in a new goroutine, blocking if the concurrency limit is reached.
func (g *Group) Go(fn func(context.Context) error) {
	g.wg.Add(1)
	if g.sem != nil {
		g.sem <- struct{}{}
	}
	go func() {
		defer g.wg.Done()
		if g.sem != nil {
			defer func() { <-g.sem }()
		}
		if err := fn(g.ctx); err != nil {
			if g.collectAll {
				g.mu.Lock()
				g.errs = append(g.errs, err)
				g.mu.Unlock()
				return
			}
			g.once.Do(func() {
				g.firstErr = err
				g.cancel()
			})
		}
	}()
}

// Wait blocks until all Go'd functions return, then reports the error: the
// first error in fail-fast mode, or errors.Join of all in collect-all mode.
func (g *Group) Wait() error {
	g.wg.Wait()
	g.cancel()
	if g.collectAll {
		return errors.Join(g.errs...)
	}
	return g.firstErr
}

// ForEach runs fn for every item with bounded concurrency, fail-fast.
func ForEach[T any](ctx context.Context, items []T, limit int, fn func(context.Context, T) error) error {
	g, _ := New(ctx, WithLimit(limit))
	for _, item := range items {
		g.Go(func(ctx context.Context) error { return fn(ctx, item) })
	}
	return g.Wait()
}

// Map applies fn to every item with bounded concurrency, fail-fast, preserving
// order. On any error it returns a nil slice and the first error.
func Map[T, U any](ctx context.Context, items []T, limit int, fn func(context.Context, T) (U, error)) ([]U, error) {
	results := make([]U, len(items))
	g, _ := New(ctx, WithLimit(limit))
	for i, item := range items {
		g.Go(func(ctx context.Context) error {
			u, err := fn(ctx, item)
			if err != nil {
				return err
			}
			results[i] = u
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}
