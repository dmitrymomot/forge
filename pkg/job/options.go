package job

import (
	"context"
	"log/slog"
)

// config holds job manager configuration.
type config struct {
	registry   *taskRegistry
	queues     map[string]int
	logger     *slog.Logger
	schedules  []scheduleConfig
	maxWorkers int
}

// newConfig creates a config with defaults.
func newConfig() *config {
	return &config{
		registry: newTaskRegistry(),
		queues:   make(map[string]int),
	}
}

// scheduleConfig holds scheduled task configuration.
//
//nolint:betteralign // all fields contain pointers, no optimization possible
type scheduleConfig struct {
	handler  scheduledHandler
	name     string
	schedule string
}

// scheduledHandler is a function type for scheduled task handlers.
type scheduledHandler func(context.Context) error

// Option configures the job manager.
type Option func(*config)

// WithTask registers a task handler using structural typing.
// The task must implement Name() and Handle(ctx, P) methods.
// The payload type P is inferred from the Handle method signature.
//
// Example:
//
//	type SendWelcome struct {
//	    mailer mail.Mailer
//	}
//
//	func (t *SendWelcome) Name() string { return "send_welcome" }
//	func (t *SendWelcome) Handle(ctx context.Context, p SendWelcomePayload) error {
//	    return t.mailer.Send(ctx, "welcome", p.Email)
//	}
//
//	job.WithTask(tasks.NewSendWelcome(mailer))
func WithTask[P any, T interface {
	Name() string
	Handle(context.Context, P) error
}](task T) Option {
	return func(c *config) {
		wrapper := newTaskWrapper[P, T](task)
		c.registry.register(task.Name(), wrapper)
	}
}

// WithScheduledTask registers a periodic task using structural typing.
// The task must implement Name(), Schedule(), and Handle(ctx) methods.
// Schedule() should return a cron expression (5 fields: min hour day month weekday).
//
// Example:
//
//	type CleanupSessions struct {
//	    repo *repository.Queries
//	}
//
//	func (t *CleanupSessions) Name() string     { return "cleanup_sessions" }
//	func (t *CleanupSessions) Schedule() string { return "0 * * * *" } // Every hour
//	func (t *CleanupSessions) Handle(ctx context.Context) error {
//	    return t.repo.DeleteExpiredSessions(ctx)
//	}
//
//	job.WithScheduledTask(tasks.NewCleanupSessions(repo))
func WithScheduledTask[T interface {
	Name() string
	Schedule() string
	Handle(context.Context) error
}](task T) Option {
	return func(c *config) {
		c.schedules = append(c.schedules, scheduleConfig{
			name:     task.Name(),
			schedule: task.Schedule(),
			handler:  task.Handle,
		})
	}
}

// WithQueue configures a named queue with the specified number of workers.
// If not specified, tasks use the default queue with default worker count.
//
// Example:
//
//	job.WithQueue("email", 10)      // 10 workers for email queue
//	job.WithQueue("reports", 2)    // 2 workers for heavy report generation
func WithQueue(name string, workers int) Option {
	return func(c *config) {
		if workers > 0 {
			c.queues[name] = workers
		}
	}
}

// WithLogger sets the logger for job processing.
// If not set, a noop logger is used.
//
// Example:
//
//	job.WithLogger(slog.Default())
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithMaxWorkers sets the default maximum number of workers.
// This applies to the default queue and any queue without explicit worker count.
// Defaults to 100 if not set.
//
// Example:
//
//	job.WithMaxWorkers(50) // Limit concurrent job processing
func WithMaxWorkers(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxWorkers = n
		}
	}
}
