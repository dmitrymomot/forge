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

// Store and Manager errors. A [Manager] layers codegen, scope, and
// redirect-time checks on top of a [Store].
var (
	// ErrNotFound is returned when a code has no Link record, or by a
	// tenant-scoped Store mutator when the code belongs to a different tenant.
	ErrNotFound = errors.New("smartlink: not found")
	// ErrDuplicate is returned by Store.Create when the code already exists.
	ErrDuplicate = errors.New("smartlink: duplicate code")
	// ErrLinkExpired is returned when a Link's ExpiresAt has passed.
	ErrLinkExpired = errors.New("smartlink: link expired")
	// ErrLinkDeactivated is returned when a Link has been deactivated.
	ErrLinkDeactivated = errors.New("smartlink: link deactivated")
	// ErrNoTarget is returned when a Link has neither a Target URL nor a Ref
	// to a compiled Spec.
	ErrNoTarget = errors.New("smartlink: no target")
	// ErrRefNotFound is the sentinel a consumer's [Cache] load func (or a
	// custom [Resolver]) wraps when a ref names no known Spec. Create's ref
	// precheck maps it (and ErrNoTarget) to ErrInvalidLink — caller input —
	// and [Manager.Handler] serves both as dead links (fallback or 404),
	// while any other resolver error propagates unwrapped as infrastructure
	// failure.
	ErrRefNotFound = errors.New("smartlink: ref not found")
	// ErrInvalidLink is returned for a malformed Link, CreateParams, or
	// Store argument (e.g. a zero Deactivate time).
	ErrInvalidLink = errors.New("smartlink: invalid link")
	// ErrCodeReserved is returned when a caller-supplied code collides with a
	// reserved value.
	ErrCodeReserved = errors.New("smartlink: code reserved")
	// ErrScope is returned when a required tenant scope is missing.
	ErrScope = errors.New("smartlink: scope required")
)
