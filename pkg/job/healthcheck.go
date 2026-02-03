package job

import (
	"context"
	"errors"
)

// ErrHealthcheckFailed is returned when the job manager health check fails.
var ErrHealthcheckFailed = errors.New("job: healthcheck failed")

var (
	errManagerNil        = errors.New("manager is nil")
	errManagerNotStarted = errors.New("manager not started")
)

// Healthcheck returns a health check function for the job manager.
// The check verifies that the manager is started and the database connection is healthy.
// Compatible with health.CheckFunc.
//
// Example:
//
//	forge.WithHealthChecks(
//	    forge.WithReadinessCheck("jobs", job.Healthcheck(manager)),
//	)
func Healthcheck(m *Manager) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if m == nil {
			return errors.Join(ErrHealthcheckFailed, errManagerNil)
		}

		m.mu.Lock()
		started := m.started
		m.mu.Unlock()

		if !started {
			return errors.Join(ErrHealthcheckFailed, errManagerNotStarted)
		}

		// Pool.Ping verifies both database connectivity and River's ability
		// to access required tables, since River uses the same pool.
		if err := m.pool.Ping(ctx); err != nil {
			return errors.Join(ErrHealthcheckFailed, err)
		}

		return nil
	}
}
