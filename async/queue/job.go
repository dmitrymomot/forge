package queue

import "time"

// Job is the queue envelope. Payload is always valid JSON. Scope is empty
// when the producing Client has no scope hook configured. Attempt is the
// number of claims so far (a job being processed for the first time has
// Attempt == 1). MaxAttempts == 0 means "use the worker's configured default".
type Job struct {
	RunAt       time.Time
	CreatedAt   time.Time
	ID          string
	Queue       string
	Type        string
	Scope       string
	LastError   string
	Payload     []byte
	Attempt     int
	MaxAttempts int
}

// ClaimedJob is a Job plus the opaque fencing token proving ownership of the
// claim. Pass Token to Extend/Ack/Nack/Kill; once the lease expires and
// another worker claims the job, ops with the stale token return ErrLeaseLost
// and leave the new claim undisturbed.
type ClaimedJob struct {
	Token string
	Job
}

// QueueStats are per-queue counts reported by Broker.Stats. Backends that
// bound their counting (postgres, cap 10000) report at most the cap and set
// the corresponding Capped flag; memory and redis counts are exact.
type QueueStats struct {
	Pending       int
	Dead          int
	PendingCapped bool
	DeadCapped    bool
}

// Stats maps queue name to its counts.
type Stats map[string]QueueStats
