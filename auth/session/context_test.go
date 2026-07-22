package session_test

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

func TestFromContextAbsent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if _, ok := session.FromContext(r.Context()); ok {
		t.Fatal("FromContext must report absent when the middleware never ran")
	}
}

func TestForReportsAbsentAndMustForPanics(t *testing.T) {
	mgr := newTestManager(t)
	r := httptest.NewRequest("GET", "/", nil)

	if _, ok := mgr.For(r); ok {
		t.Fatal("For must report ok=false when the middleware is not mounted")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("MustFor must panic when the middleware is not mounted")
		}
	}()
	_ = mgr.MustFor(r)
}

func TestInfoTracksMidRequestAuthenticate(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()

	ctx := session.TestWithSession(t.Context(), sess)
	info, ok := session.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext must find the session the middleware stored")
	}
	if info.Authenticated() {
		t.Fatal("a fresh session must not report Authenticated")
	}

	if err := mgr.Authenticate(ctx, sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if info.UserID != "u1" {
		t.Fatalf("Info.UserID = %q after mid-request Authenticate, want u1 — Info must not go stale", info.UserID)
	}
	if !info.Authenticated() {
		t.Fatal("Info must report Authenticated after Authenticate")
	}
}

func TestManagerForNilRequestDoesNotPanic(t *testing.T) {
	mgr := newTestManager(t)
	if _, ok := mgr.For(nil); ok {
		t.Fatal("For(nil) must report ok=false, not panic")
	}
}

func TestInfoAuthenticatedNilReceiver(t *testing.T) {
	var info *session.Info
	if info.Authenticated() {
		t.Fatal("Authenticated on a nil *Info must report false, not panic")
	}
}

func TestLogExtractorAbsentSession(t *testing.T) {
	if _, ok := session.LogExtractor(t.Context()); ok {
		t.Fatal("LogExtractor must report ok=false when there is no session in context")
	}
}

func TestLogExtractorAnonymousSession(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	ctx := session.TestWithSession(t.Context(), sess)

	attr, ok := session.LogExtractor(ctx)
	if !ok {
		t.Fatal("LogExtractor must report ok=true once a non-zero session id is present")
	}
	if attr.Key != "session" || attr.Value.Kind() != slog.KindGroup {
		t.Fatalf("attr = %+v, want a %q group", attr, "session")
	}
	group := attr.Value.Group()
	if len(group) != 1 || group[0].Key != "id" {
		t.Fatalf("anonymous session group = %+v, want only an %q attr — no user, no token", group, "id")
	}
	for _, a := range group {
		if a.Value.String() == sess.Token() {
			t.Fatal("LogExtractor must never log the raw session token")
		}
	}
}

func TestLogExtractorAuthenticatedSessionIncludesUser(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	ctx := session.TestWithSession(t.Context(), sess)

	if err := mgr.Authenticate(ctx, sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	attr, ok := session.LogExtractor(ctx)
	if !ok {
		t.Fatal("LogExtractor must report ok=true for an authenticated session")
	}
	group := attr.Value.Group()
	var sawUser bool
	for _, a := range group {
		if a.Key == "user" {
			sawUser = true
			if a.Value.String() != "u1" {
				t.Fatalf("user attr = %q, want u1", a.Value.String())
			}
		}
		if a.Value.String() == sess.Token() {
			t.Fatal("LogExtractor must never log the raw session token")
		}
	}
	if !sawUser {
		t.Fatal("LogExtractor must include a user attr once a principal is bound")
	}
}
