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

// WithQueue specifies which queue to use for the job.
// If not specified, the default queue is used.
//
// Example:
//
//	c.Enqueue("send_email", payload, job.WithQueue("email"))
func WithQueue(name string) EnqueueOption {
	return func(c *enqueueConfig) {
		if name != "" {
			c.queue = name
		}
	}
}

// WithScheduledAt schedules the job to run at a specific time.
// The job will not be processed until this time.
//
// Example:
//
//	tomorrow := time.Now().Add(24 * time.Hour)
//	c.Enqueue("send_reminder", payload, job.WithScheduledAt(tomorrow))
func WithScheduledAt(t time.Time) EnqueueOption {
	return func(c *enqueueConfig) {
		c.scheduledAt = &t
	}
}

// WithScheduledIn schedules the job to run after a duration.
// The job will not be processed until this duration has passed.
//
// Example:
//
//	c.Enqueue("send_reminder", payload, job.WithScheduledIn(24*time.Hour))
func WithScheduledIn(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		t := time.Now().Add(d)
		c.scheduledAt = &t
	}
}

// WithMaxAttempts sets the maximum number of retry attempts for the job.
// If the job fails, it will be retried up to this many times.
// Defaults to River's default (25 attempts).
//
// Example:
//
//	c.Enqueue("process_payment", payload, job.WithMaxAttempts(3))
func WithMaxAttempts(n int) EnqueueOption {
	return func(c *enqueueConfig) {
		if n > 0 {
			c.maxAttempts = n
		}
	}
}

// WithUniqueFor ensures only one job with this key exists for the specified duration.
// If a job with the same key and task name already exists, the new job is skipped.
// This is useful for preventing duplicate job processing.
//
// Example:
//
//	// Only one password reset email per user per hour
//	c.Enqueue("send_password_reset", payload,
//	    job.WithUniqueFor(time.Hour),
//	    job.WithUniqueKey(userID))
func WithUniqueFor(d time.Duration) EnqueueOption {
	return func(c *enqueueConfig) {
		c.uniqueFor = d
	}
}

// WithUniqueKey sets a custom unique key for deduplication.
// Combined with WithUniqueFor, this prevents duplicate jobs with the same key.
// If not set, River generates a key based on the job arguments.
//
// Example:
//
//	c.Enqueue("sync_user", payload,
//	    job.WithUniqueFor(5*time.Minute),
//	    job.WithUniqueKey(userID))
func WithUniqueKey(key string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.uniqueKey = key
	}
}

// WithPriority sets the job priority (lower numbers = higher priority).
// Jobs with lower priority values are processed first.
// Defaults to 1 if not set.
//
// Example:
//
//	c.Enqueue("urgent_task", payload, job.WithPriority(0))  // Highest priority
//	c.Enqueue("normal_task", payload, job.WithPriority(1))  // Normal priority
//	c.Enqueue("bulk_task", payload, job.WithPriority(10))   // Lower priority
func WithPriority(p int) EnqueueOption {
	return func(c *enqueueConfig) {
		c.priority = p
	}
}

// WithTags adds metadata tags to the job.
// Tags can be used for filtering, monitoring, and debugging.
//
// Example:
//
//	c.Enqueue("send_email", payload,
//	    job.WithTags("email", "marketing", "campaign:123"))
func WithTags(tags ...string) EnqueueOption {
	return func(c *enqueueConfig) {
		c.tags = append(c.tags, tags...)
	}
}
