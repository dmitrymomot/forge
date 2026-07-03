package sanitize

// Apply runs transforms left-to-right over value and returns the result. It is
// generic so non-string transform chains compose too. With no transforms it
// returns value unchanged.
func Apply[T any](value T, transforms ...func(T) T) T {
	for _, t := range transforms {
		value = t(value)
	}
	return value
}

// Compose returns a reusable pipeline that applies transforms left-to-right, in
// the same order as Apply. The zero-transform pipeline is the identity.
func Compose[T any](transforms ...func(T) T) func(T) T {
	return func(value T) T {
		return Apply(value, transforms...)
	}
}
