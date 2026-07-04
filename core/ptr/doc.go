// Package ptr provides generic pointer helpers (From, FromOr, Equal) for
// optional struct fields, JSON omitempty, and SQL nullables, plus Optional[T],
// a two-state "provided?" wrapper for JSON PATCH semantics.
// A pointer to a literal is the Go 1.26 builtin new(expr), so ptr does not wrap it.
//
// # Usage
//
//	n := ptr.From(new(7))     // 7
//	z := ptr.From[int](nil)   // 0
//	d := ptr.FromOr[int](nil, 99) // 99
//
//	var patch struct {
//		Name ptr.Optional[string] `json:"name,omitzero"`
//	}
//	json.Unmarshal([]byte(`{"name":"hi"}`), &patch)
//	if v, ok := patch.Name.Get(); ok {
//		_ = v // "hi"
//	}
package ptr
