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

// QueueStats are per-queue counts reported by Broker.Stats.
type QueueStats struct {
	Pending int
	Dead    int
}

// Stats maps queue name to its counts.
type Stats map[string]QueueStats
