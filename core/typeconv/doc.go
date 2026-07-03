// Package typeconv converts strings to Go scalars and back without reflection.
//
// It is the scalar substrate that envconfig, form decoding, and featureflag
// build field decoders on: Parse[T] and the ParseInt/ParseUint/... helpers turn
// a string into a typed value; Format is the lossless inverse. Struct-field
// walking belongs to structfields; locale-aware parsing belongs to i18n.
//
// Parse[T] dispatches on the exact base kind, so a defined type (type Status
// string) will not match generic Parse — numeric defined types are served by
// the constraint helpers (ParseInt[MyInt]) and string-defined types by a
// trivial conversion. Time is RFC3339 both ways.
package typeconv
