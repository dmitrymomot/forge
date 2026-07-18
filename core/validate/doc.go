// Package validate provides reflection-free, composable value validation. A rule is
// Rule[T] = func(T) Violation (the zero Violation means "passed"); failures carry a
// stable i18n key ("validation.<rule>") plus []Param, never a baked-in English
// sentence.
//
// Param-less rules are used bare (validate.Email, validate.Required); parameterized
// rules are constructors that return a Rule[T] (validate.MinLen(2)). Apply runs a
// field's rules against its value; Check aggregates the field Results into one error.
//
// Rules compose into rules via And/Or/Not/Each/Msg/WithKey/When; conditionals use
// When (check-level) and WhenField (field-level).
//
// What this is NOT: not a struct-tag validator (no reflection, no `validate:"..."`
// DSL — rules are composed explicitly); it does not render messages (it emits
// keys+params for a downstream i18n layer, though the literal Msg override is
// interpolated in-package); and ETHAddress is structural only — EIP-55 checksum
// verification is out of scope (it needs Keccak, which would end validate's
// stdlib-only status). Untrusted-input normalization belongs to the sanitize
// package; slug generation to the slug package.
//
// # Usage
//
//	age := 15
//	email := "invalid"
//	err := validate.Check(
//		validate.Apply("age", age, validate.Min(18)),
//		validate.Apply("email", email, validate.Msg(validate.Email, "invalid email")),
//	)
//	// err.Error() == "age: validation.min; email: invalid email"
package validate
