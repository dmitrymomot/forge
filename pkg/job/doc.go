// Package job provides background job processing using River (Postgres-native queue).
//
// This package enables asynchronous task execution with features like retry handling,
// scheduled jobs, transactional enqueueing, and multiple queue support. It wraps River
// to provide a simplified, type-safe API that integrates seamlessly with the Forge framework.
//
// # Features
//
//   - Type-safe task registration with structural typing (no interface imports needed)
//   - Scheduled/periodic tasks with cron expressions
//   - Transactional job enqueueing (jobs only visible after commit)
//   - Multiple named queues with configurable worker counts
//   - Automatic retry with exponential backoff
//   - Job deduplication with uniqueness constraints
//   - Priority-based job ordering
//   - Health check integration
//
// # Task Definition
//
// Tasks are defined as structs with Name() and Handle() methods.
// No interface import is required - the package uses structural typing:
//
//	type SendWelcome struct {
//	    mailer mail.Mailer
//	    repo   *repository.Queries
//	}
//
//	func NewSendWelcome(mailer mail.Mailer, repo *repository.Queries) *SendWelcome {
//	    return &SendWelcome{mailer: mailer, repo: repo}
//	}
//
//	func (t *SendWelcome) Name() string { return "send_welcome" }
//
//	func (t *SendWelcome) Handle(ctx context.Context, p SendWelcomePayload) error {
//	    user, err := t.repo.GetUser(ctx, p.UserID)
//	    if err != nil {
//	        return err
//	    }
//	    return t.mailer.Send(ctx, "welcome", user.Email, user)
//	}
//
//	type SendWelcomePayload struct {
//	    UserID string `json:"user_id"`
//	}
//
// # Scheduled Tasks
//
// Periodic tasks implement Schedule() returning a cron expression:
//
//	type CleanupSessions struct {
//	    repo *repository.Queries
//	}
//
//	func (t *CleanupSessions) Schedule() string { return "0 * * * *" } // Every hour
//
//	func (t *CleanupSessions) Handle(ctx context.Context) error {
//	    return t.repo.DeleteExpiredSessions(ctx)
//	}
//
// # App Integration
//
// Jobs integrate with Forge through the WithJobs option:
//
//	import (
//	    "github.com/dmitrymomot/forge"
//	    "github.com/dmitrymomot/forge/pkg/job"
//	)
//
//	app := forge.New(
//	    forge.WithJobs(pool,
//	        job.WithTask(tasks.NewSendWelcome(mailer, repo)),
//	        job.WithTask(tasks.NewProcessPayment(stripe, repo)),
//	        job.WithScheduledTask(tasks.NewCleanupSessions(repo), "cleanup_sessions"),
//	        job.WithQueue("email", 10),
//	        job.WithQueue("payments", 5),
//	        job.WithLogger(slog.Default()),
//	    ),
//	)
//
// # Enqueueing Jobs
//
// Jobs are enqueued from handlers using the Context methods:
//
//	func (h *UserHandler) Create(c forge.Context) error {
//	    // ... create user ...
//
//	    // Simple enqueue
//	    err := c.Enqueue("send_welcome", tasks.SendWelcomePayload{
//	        UserID: user.ID,
//	    })
//
//	    // With options
//	    err := c.Enqueue("send_reminder", payload,
//	        job.ScheduledIn(24*time.Hour),
//	        job.InQueue("email"),
//	        job.MaxAttempts(3),
//	    )
//
//	    return c.JSON(http.StatusCreated, user)
//	}
//
// # Transactional Enqueueing
//
// For atomicity between database changes and job enqueueing:
//
//	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
//	    user, err := repo.CreateUser(ctx, tx, req)
//	    if err != nil {
//	        return err
//	    }
//
//	    // Job only exists if transaction commits
//	    return c.EnqueueTx(tx, "send_welcome", tasks.SendWelcomePayload{
//	        UserID: user.ID,
//	    })
//	})
//
// # Job Uniqueness
//
// Prevent duplicate job processing with uniqueness options:
//
//	// Only one password reset per user per hour
//	c.Enqueue("send_password_reset", payload,
//	    job.UniqueFor(time.Hour),
//	    job.UniqueKey(userID),
//	)
//
// # Health Checks
//
// Add job manager health check to readiness probes:
//
//	forge.WithHealthChecks(
//	    forge.WithReadinessCheck("db", db.Healthcheck(pool)),
//	    forge.WithReadinessCheck("jobs", job.Healthcheck(manager)),
//	)
//
// # Error Handling
//
// The package defines sentinel errors for common failure modes:
//
//   - [ErrNotConfigured] - WithJobs was not called
//   - [ErrUnknownTask] - Task name not registered
//   - [ErrInvalidPayload] - Payload deserialization failed
//   - [ErrAlreadyStarted] - Manager already running
//   - [ErrNotStarted] - Manager not running
//   - [ErrHealthcheckFailed] - Health check failed
//
// # Database Migrations
//
// River requires database tables. Run River migrations before using:
//
//	CREATE TABLE river_job (...);
//	CREATE TABLE river_leader (...);
//	CREATE TABLE river_queue (...);
//
// See River documentation for migration SQL: https://riverqueue.com/docs/migrations
package job
