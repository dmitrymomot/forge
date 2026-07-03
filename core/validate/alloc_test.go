package validate_test

import (
	"testing"

	"github.com/dmitrymomot/forge/core/validate"
)

// The happy path must not allocate: bare param-less rules are plain function values,
// a predefined parameterized rule is built once, and Apply's non-escaping variadic
// stack-allocates. Regression guard for the measured 0-allocs/op claim.
//
// Note: the rules exercised here are pure (ASCII/NotBlank/MinLen). Rules backed by a
// stdlib parser that allocates internally (e.g. Email via net/mail, or the regexp-
// backed UUID/Numeric) are intentionally excluded — their non-zero allocs come from
// the stdlib call, not from validate's Rule/Apply machinery, which this guards.
func TestZeroAllocHappyPath(t *testing.T) {
	code := "ABC123"
	name := "abc"
	minLen2 := validate.MinLen(2) // predefined once, reused

	allocs := testing.AllocsPerRun(1000, func() {
		_ = validate.Apply("code", code, validate.ASCII)             // bare param-less rule
		_ = validate.Apply("name", name, validate.NotBlank, minLen2) // bare + predefined
	})
	if allocs != 0 {
		t.Fatalf("happy path must be zero-alloc, got %v allocs/op", allocs)
	}
}
