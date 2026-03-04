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

	// ErrDriverRequired is returned when attempting to create a manager
	// or enqueuer without providing a driver.
	ErrDriverRequired = errors.New("job: driver is required")

	// ErrInvalidTx is returned when the wrong transaction type is passed
	// to InsertTx. Each driver expects its own transaction type
	// (e.g., pgx.Tx for River, *sql.Tx for SQLite).
	ErrInvalidTx = errors.New("job: invalid transaction type for this driver")

	// ErrPoolRequired is kept for backward compatibility.
	// Deprecated: Use ErrDriverRequired instead.
	ErrPoolRequired = ErrDriverRequired
)
