package lock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

// RunOnLeader returns a supervisor.Service that runs run on exactly one node.
// It continuously campaigns for key; whoever holds the lease runs
// run(leaderCtx) while the rest stand by. leaderCtx is cancelled on leadership
// loss (run must return promptly). On supervisor shutdown it releases the lease
// for instant failover. Election is automatic and continuous.
func (l *Lock) RunOnLeader(name, key string, run func(ctx context.Context) error) supervisor.Service {
	return &leader{lock: l, name: name, key: key, run: run}
}

type leader struct {
	lock *Lock
	run  func(context.Context) error
	name string
	key  string
}

func (le *leader) Name() string { return le.name }

func (le *leader) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return fmt.Errorf("lock: run on leader %q: %w", le.name, ctx.Err())
		}
		lease, err := le.lock.Acquire(ctx, le.key)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("lock: run on leader %q: %w", le.name, ctx.Err()) // cancelled while campaigning = clean stop
			}
			return err
		}

		leaderCtx, cancel := context.WithCancel(ctx)
		watchDone := make(chan struct{})
		go func() {
			defer close(watchDone)
			select {
			case <-lease.Done(): // lost leadership
				cancel()
			case <-leaderCtx.Done():
			}
		}()

		runErr := le.run(leaderCtx)
		cancel()
		<-watchDone // ensure the watcher goroutine has exited before re-campaigning
		le.release(ctx, lease)

		if ctx.Err() != nil {
			return fmt.Errorf("lock: run on leader %q: %w", le.name, ctx.Err())
		}
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return runErr // a real error stops the service
		}
		// lost leadership but parent alive → re-campaign
	}
}

// release frees the lease even when the parent ctx is already cancelled, so
// shutdown yields the lock immediately (instant failover) instead of after TTL.
func (le *leader) release(parent context.Context, lease *Lease) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	_ = lease.Release(rctx)
}
