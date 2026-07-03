// Package structfields is forge's single sanctioned reflection helper. Walk
// visits each exported field of a struct (or non-nil *struct) exactly once,
// handing the caller the field name, its parsed struct tag (Tag), the field's
// reflect.Value, and a Set closure — so consumers like envconfig, form binding,
// and row scanning stay reflection-free themselves by depending on this one
// audited primitive.
//
// A non-nil *struct yields settable fields (Set assigns/converts into the
// value); a value struct walks read-only (Set returns ErrNotSettable). Any
// other input returns ErrNotStruct.
//
// What this is NOT: it does not flatten anonymous embedded structs — an
// embedded struct is yielded as one field, and a caller needing recursion
// re-Walks that field's value (embedded flattening + name prefixing may be
// added later without an API break). It visits only exported fields. It does
// not bind or populate structs from external data, does not validate, and does
// not parse scalar values — struct-tag binding lives in the consumers, scalar
// conversion in typeconv, and value validation in validate.
package structfields
