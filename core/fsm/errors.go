package fsm

import "errors"

var (
	// ErrInvalidDefinition reports construction-time validation failure; its message aggregates every issue found on a single line.
	ErrInvalidDefinition = errors.New("fsm: invalid definition")

	// ErrUnknownState reports a state outside the machine's declared set.
	ErrUnknownState = errors.New("fsm: unknown state")

	// ErrIllegalTransition reports a from -> to pair with no declared edge.
	ErrIllegalTransition = errors.New("fsm: illegal transition")

	// ErrGuardDenied reports a guard refusal; the guard's own error is wrapped alongside and survives errors.Is/As.
	ErrGuardDenied = errors.New("fsm: guard denied")

	// ErrHookFailed reports a hook error; the hook's own error is wrapped alongside and survives errors.Is/As.
	ErrHookFailed = errors.New("fsm: hook failed")
)
