package job

import "time"

// enqueueConfig holds options for enqueueing a job.
type enqueueConfig struct {
	scheduledAt *time.Time
	queue       string
	uniqueKey   string
	tags        []string
	maxAttempts int
	uniqueFor   time.Duration
	priority    int
}

// EnqueueOption configures job enqueueing.
type EnqueueOption func(*enqueueConfig)

// InQueue specifies which queue to use for the job.
// If not specified, the default queue is used.
//
// Example:
//
//	c.Enqueue("send_email", payload, job.InQueue("email"))
func InQueue(name string) EnqueueOption {
	return func(c *enqueueConfig) {
		if name != "" {
			c.queue = name
		}
	}
}

// ScheduledAt schedules the job to run at a specific time.
// The job will not be processed until this time.
//
// Example:
//
//	tomorrow := time.Now().Add(24 * time.Hour)
//	c.Enqueue("send_reminder", payload, job.ScheduledAt(tomorrow))
func ScheduledAt(t time.Time) EnqueueOption {
	return func(c *enqueueConfig) {
		c.scheduledAt = &t
	}
}

// ScheduledIn schedules the job to run after a duration.
// The job will not be processed until this duration has passed.
//
// Example:
//
//	c.Enqueue("send_reminder", payload, job.ScheduledIn(24*time.Hour))
func ScheduledIn(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		t := time.Now().Add(d)
		c.scheduledAt = &t
	}
}

// MaxAttempts sets the maximum number of retry attempts for the job.
// If the job fails, it will be retried up to this many times.
// Defaults to River's default (25 attempts).
//
// Example:
//
//	c.Enqueue("process_payment", payload, job.MaxAttempts(3))
func MaxAttempts(n int) EnqueueOption {
	return func(c *enqueueConfig) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}

// UniqueFor ensures only one job with this key exists for the specified duration.
// If a job with the same key and task name already exists, the new job is skipped.
// This is useful for preventing duplicate job processing.
//
// Example:
//
//	// Only one password reset email per user per hour
//	c.Enqueue("send_password_reset", payload,
//	    job.UniqueFor(time.Hour),
//	    job.UniqueKey(userID))
func UniqueFor(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		c.uniqueFor = d
	}
}

// UniqueKey sets a custom unique key for deduplication.
// Combined with UniqueFor, this prevents duplicate jobs with the same key.
// If not set, River generates a key based on the job arguments.
//
// Example:
//
//	c.Enqueue("sync_user", payload,
//	    job.UniqueFor(5*time.Minute),
//	    job.UniqueKey(userID))
func UniqueKey(key string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.uniqueKey = key
	}
}

// Priority sets the job priority (lower numbers = higher priority).
// Jobs with lower priority values are processed first.
// Defaults to 1 if not set.
//
// Example:
//
//	c.Enqueue("urgent_task", payload, job.Priority(0))  // Highest priority
//	c.Enqueue("normal_task", payload, job.Priority(1))  // Normal priority
//	c.Enqueue("bulk_task", payload, job.Priority(10))   // Lower priority
func Priority(p int) EnqueueOption {
	return func(c *enqueueConfig) {
		c.priority = p
	}
}

// Tags adds metadata tags to the job.
// Tags can be used for filtering, monitoring, and debugging.
//
// Example:
//
//	c.Enqueue("send_email", payload,
//	    job.Tags("email", "marketing", "campaign:123"))
func Tags(tags ...string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.tags = append(c.tags, tags...)
	}
}
