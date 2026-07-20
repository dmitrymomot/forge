package abac

import "errors"

var (
	// ErrUnnamedRule is returned by New for a rule with an empty name (including
	// the zero Rule). Names label decisions in the explanation record.
	ErrUnnamedRule = errors.New("abac: rule has no name")

	// ErrDuplicateRule is returned by New when two rules share a name — names
	// must be unique so an explanation points at exactly one rule.
	ErrDuplicateRule = errors.New("abac: duplicate rule name")

	// ErrNilPredicate is returned by New for a rule with a nil predicate.
	ErrNilPredicate = errors.New("abac: nil predicate")

	// ErrEmptyAction is returned by New for a rule with an empty action pattern.
	ErrEmptyAction = errors.New("abac: rule has no action pattern")

	// ErrEmptyResource is returned by New for a rule with an empty resource
	// type. Matching any resource type must be the explicit "*".
	ErrEmptyResource = errors.New("abac: rule has no resource type")
)
