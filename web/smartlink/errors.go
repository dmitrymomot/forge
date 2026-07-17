package smartlink

import "errors"

// Compile-time validation errors. Match with errors.Is; Compile wraps each with
// the offending rule, target, or matcher context. Decide never returns an error.
var (
	// ErrNoDefault is returned when a Spec has no default target.
	ErrNoDefault = errors.New("smartlink: no default target")
	// ErrInvalidRule is returned for a rule with an empty or duplicate name, or no targets.
	ErrInvalidRule = errors.New("smartlink: invalid rule")
	// ErrInvalidTarget is returned for a target with an empty URL or a bad weight.
	ErrInvalidTarget = errors.New("smartlink: invalid target")
	// ErrInvalidMatcher is returned for a matcher with empty or out-of-range values.
	ErrInvalidMatcher = errors.New("smartlink: invalid matcher")
	// ErrInvalidTemplate is returned for a malformed target URL template.
	ErrInvalidTemplate = errors.New("smartlink: invalid template")
	// ErrUnknownMacro is returned for a template macro outside the vocabulary.
	ErrUnknownMacro = errors.New("smartlink: unknown macro")
)
