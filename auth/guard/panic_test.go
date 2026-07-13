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

func TestWithExtractors_NilElementPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "WithExtractors(nil)", func() {
		guard.New(okVerifier(), guard.WithExtractors(nil))
	})
}

func TestBasicAuth_EmptyUsersPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "BasicAuth(nil)", func() { guard.BasicAuth(nil) })
	mustPanic(t, "BasicAuth(empty)", func() { guard.BasicAuth(map[string]string{}) })
}

func TestBasicAuth_EmptyCredentialPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, "BasicAuth(empty password)", func() { guard.BasicAuth(map[string]string{"admin": ""}) })
	mustPanic(t, "BasicAuth(empty username)", func() { guard.BasicAuth(map[string]string{"": "pw"}) })
}

func TestWithRealm_InvalidPanics(t *testing.T) {
	t.Parallel()
	mustPanic(t, `WithRealm(quote)`, func() { guard.WithRealm(`sta"ging`) })
	mustPanic(t, "WithRealm(control)", func() { guard.WithRealm("sta\nging") })
}
