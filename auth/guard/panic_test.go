package guard_test

import (
	"testing"

	"github.com/dmitrymomot/forge/auth/guard"
)

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", name)
		}
	}()
	fn()
}

func TestNew_NilVerifierPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "New(nil)", func() { guard.New(nil) })
}

func TestWithExtractors_EmptyPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "WithExtractors()", func() {
		guard.New(okVerifier(), guard.WithExtractors())
	})
}
