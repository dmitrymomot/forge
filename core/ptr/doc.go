// Package ptr provides generic pointer helpers (From, FromOr, Equal) for
// optional struct fields, JSON omitempty, and SQL nullables, plus Optional[T],
// a two-state "provided?" wrapper for JSON PATCH semantics.
// A pointer to a literal is the Go 1.26 builtin new(expr), so ptr does not wrap it.
package ptr
