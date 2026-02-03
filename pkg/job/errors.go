package job

import "errors"

// Job errors.
var (
	// ErrNotConfigured is returned when job functionality is used
	// but WithJobs was not configured on the app.
	ErrNotConfigured = errors.New("job: not configured")

	// ErrUnknownTask is returned when attempting to execute a task
	// that has not been registered.
	ErrUnknownTask = errors.New("job: unknown task")

	// ErrInvalidPayload is returned when a task payload cannot be
	// unmarshaled into the expected type.
	ErrInvalidPayload = errors.New("job: invalid payload")

	// ErrAlreadyStarted is returned when attempting to start a manager
	// that is already running.
	ErrAlreadyStarted = errors.New("job: already started")

	// ErrNotStarted is returned when attempting to stop a manager
	// that is not running.
	ErrNotStarted = errors.New("job: not started")

	// ErrPoolRequired is returned when attempting to create a manager
	// or enqueuer without providing a database pool.
	ErrPoolRequired = errors.New("job: pool is required")
)
