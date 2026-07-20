package postback

import "errors"

var (
	// ErrInvalidMacro means a vocabulary name is empty or carries characters
	// outside letters, digits, '_', '.', and '-'.
	ErrInvalidMacro = errors.New("postback: invalid macro name")

	// ErrUnknownMacro means a template references a macro the vocabulary does
	// not register — caught at NewDestination, never an empty substitution at
	// fire time.
	ErrUnknownMacro = errors.New("postback: unknown macro")

	// ErrInvalidTemplate means the destination URL template is malformed:
	// unbalanced braces, not an absolute http(s) URL, a fragment (never sent
	// to the server), or a macro in the authority.
	ErrInvalidTemplate = errors.New("postback: invalid template")

	// ErrInvalidMethod means the destination method is neither GET nor POST.
	ErrInvalidMethod = errors.New("postback: invalid method")

	// ErrClientStatus means the tracker answered with a non-2xx, non-5xx
	// status — the destination or event is wrong; retrying won't help.
	ErrClientStatus = errors.New("postback: client-error status")

	// ErrServerStatus means the tracker answered 5xx — transient, worth
	// retrying.
	ErrServerStatus = errors.New("postback: server-error status")
)
