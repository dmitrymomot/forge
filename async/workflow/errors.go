package workflow

import (
	"errors"
	"fmt"
)

var (
	// ErrRunNotFound is returned by Store.Get and Store.Update for an unknown run id.
	ErrRunNotFound = errors.New("workflow: run not found")
	// ErrRunAlreadyExists is returned by Start (via Store.Create) when the run id is taken — the WithRunID dedupe signal.
	ErrRunAlreadyExists = errors.New("workflow: run already exists")
	// ErrStaleRun is returned by Store.Update when the stored version does not match: another worker checkpointed this run first.
	ErrStaleRun = errors.New("workflow: stale run version")
	// ErrScopeMissing is returned by Start when a scope hook is configured and yields an error or empty scope.
	ErrScopeMissing = errors.New("workflow: scope missing")
	// ErrNotRegistered is returned by Start when the workflow was not registered on the engine.
	ErrNotRegistered = errors.New("workflow: workflow not registered")
	// ErrNoWorkflows is returned by NewService when nothing was registered.
	ErrNoWorkflows = errors.New("workflow: no workflows registered")
)

type failError struct{ err error }

func (e *failError) Error() string { return fmt.Sprintf("workflow: permanent failure: %v", e.err) }
func (e *failError) Unwrap() error { return e.err }

// Fail wraps err into a step verdict: the failure is permanent — no retry can
// help — so stop retrying this step and start compensating. Fail(nil) returns
// nil. Steps may equivalently return queue.SkipRetry or queue.Cancel; the
// engine treats all three as permanent so a stray queue verdict can never
// dead-letter the driving job while the run still looks alive.
func Fail(err error) error {
	if err == nil {
		return nil
	}
	return &failError{err: err}
}

// IsFail reports whether err carries the Fail verdict.
func IsFail(err error) bool {
	var f *failError
	return errors.As(err, &f)
}
