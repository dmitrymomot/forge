package scheduler

import "errors"

var (
	// ErrInvalidConfig is returned by Config.Validate and New on bad configuration.
	ErrInvalidConfig = errors.New("scheduler: invalid config")
	// ErrInvalidSpec is returned by Cron and CronIn on a malformed cron expression.
	ErrInvalidSpec = errors.New("scheduler: invalid cron spec")
	// ErrAlreadyClaimed is returned by Store.Claim when another instance already claimed the tick.
	ErrAlreadyClaimed = errors.New("scheduler: tick already claimed")
	// ErrNoJobs is returned by Run when nothing was added.
	ErrNoJobs = errors.New("scheduler: no jobs added")
	// ErrAlreadyRunning is returned by Run when the scheduler is already running.
	ErrAlreadyRunning = errors.New("scheduler: already running")
)
