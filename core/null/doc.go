// Package null provides Null[T], a single generic nullable that round-trips
// through database/sql (via the embedded sql.Null[T]) and encoding/json
// (marshaling as JSON null when not valid), replacing the sql.NullString /
// NullInt64 family.
//
// T must be a type database/sql's conversion supports (the scalar kinds,
// time.Time, []byte, string). A JSON-column nullable over an arbitrary struct
// needs its own sql.Scanner and is out of scope. Null[T] models a SQL/JSON null
// value; ptr.Optional models JSON PATCH absence — they do not overlap.
//
// # Usage
//
//	n := null.Of(42)
//	v, ok := n.Get() // 42, true
//
//	e := null.Empty[int]()
//	p := e.Ptr() // nil
//
//	b, _ := json.Marshal(n) // "42"
package null
