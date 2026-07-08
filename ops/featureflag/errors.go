package featureflag

import "errors"

var (
	// ErrEmptyKey reports an empty flag key in a source or option.
	ErrEmptyKey = errors.New("featureflag: empty flag key")
	// ErrInvalidRollout reports a rollout percent outside 0-100.
	ErrInvalidRollout = errors.New("featureflag: rollout must be between 0 and 100")
	// ErrUnknownFlag reports an adjuster option targeting a key absent from the static set.
	ErrUnknownFlag = errors.New("featureflag: unknown flag")
	// ErrInvalidFlag reports a malformed flag definition.
	ErrInvalidFlag = errors.New("featureflag: invalid flag definition")
	// ErrNilProvider reports a nil Provider passed to WithProvider or Cached.
	ErrNilProvider = errors.New("featureflag: nil provider")
)
