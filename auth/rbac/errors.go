package rbac

import "errors"

var (
	// ErrDuplicateRole is returned by NewRoleSet when a role name is declared
	// more than once via WithRoles.
	ErrDuplicateRole = errors.New("rbac: duplicate role")

	// ErrUnknownRole is returned by NewRoleSet when an inheritance edge names a
	// role not defined via WithRoles (child or parent).
	ErrUnknownRole = errors.New("rbac: unknown role in inheritance edge")

	// ErrCycle is returned by NewRoleSet when the inheritance graph contains a
	// cycle.
	ErrCycle = errors.New("rbac: inheritance cycle")

	// ErrScope fails a Manager operation closed when the WithScope hook errors
	// or yields an empty tenant.
	ErrScope = errors.New("rbac: scope hook failed")
)
