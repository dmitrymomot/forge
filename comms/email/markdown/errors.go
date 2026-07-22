package markdown

import "errors"

var (
	// ErrInvalidDocument means the source is not a renderable email document:
	// missing or malformed YAML frontmatter, an unknown frontmatter key (typo
	// — fail closed), or a missing/multi-line subject.
	ErrInvalidDocument = errors.New("email/markdown: invalid document")

	// ErrInvalidLayout is returned by New when a custom layout does not parse
	// or does not execute.
	ErrInvalidLayout = errors.New("email/markdown: invalid layout")
)
