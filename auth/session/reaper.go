package session

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

type reaper struct {
	mgr   *Manager
	every time.Duration
}

// Reaper returns a supervisor.Service that periodically deletes expired records. On a store that expires records natively it logs once and waits for cancellation rather than failing the supervisor.
func Reaper(m *Manager, every time.Duration) supervisor.Service {
	if every <= 0 {
		every = 15 * time.Minute
	}
	return &reaper{mgr: m, every: every}
}

// Name implements supervisor.Service.
func (r *reaper) Name() string { return "session-reaper" }

// Run implements supervisor.Service.
func (r *reaper) Run(ctx context.Context) error {
	if r.mgr.expirer == nil {
		r.mgr.log.InfoContext(ctx, "session: store reaps expired records natively; reaper is a no-op")
		<-ctx.Done()
		return nil
	}

	t := time.NewTicker(r.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			n, err := r.mgr.DeleteExpired(ctx)
			switch {
			case errors.Is(err, context.Canceled):
				return nil
			case err != nil:
				r.mgr.log.ErrorContext(ctx, "session: reaping expired records failed", slog.Any("error", err))
			case n > 0:
				r.mgr.log.DebugContext(ctx, "session: reaped expired records", slog.Int("count", n))
			}
		}
	}
}
