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

func TestInfoStaysStaleAfterFailedAuthenticate(t *testing.T) {
	store := &failingSaveStore{MemoryStore: session.NewMemoryStore()}
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	ctx := session.TestWithSession(t.Context(), sess)
	info, ok := session.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext must find the session the middleware stored")
	}

	store.failOn = store.calls + 1
	if err := mgr.Authenticate(ctx, sess, "u1"); err == nil {
		t.Fatal("Authenticate must surface the store failure")
	}

	if info.UserID != "" {
		t.Fatalf("Info.UserID = %q after a failed Authenticate, want empty — a rolled-back save must not leave Info showing the attempted user", info.UserID)
	}
	if info.Authenticated() {
		t.Fatal("Info must not report Authenticated after a failed Authenticate")
	}
}

func TestInfoTracksMidRequestElevate(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	ctx := session.TestWithSession(t.Context(), sess)
	info, ok := session.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext must find the session the middleware stored")
	}
	if !info.ElevatedAt.IsZero() {
		t.Fatal("a fresh session's Info must not report an elevation stamp")
	}

	if err := mgr.Elevate(ctx, sess); err != nil {
		t.Fatalf("Elevate: %v", err)
	}

	if info.ElevatedAt.IsZero() {
		t.Fatal("Elevate must stamp a non-zero ElevatedAt onto Info")
	}
	if !info.ElevatedAt.Equal(sess.ElevatedAt()) {
		t.Fatalf("Info.ElevatedAt = %v after mid-request Elevate, want %v — Info must not go stale", info.ElevatedAt, sess.ElevatedAt())
	}
}

func TestInfoStaysConsistentAfterMidRequestRotate(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	ctx := session.TestWithSession(t.Context(), sess)
	info, ok := session.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext must find the session the middleware stored")
	}

	if err := mgr.Authenticate(ctx, sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	wantID, wantUser := sess.ID(), sess.UserID()

	if err := mgr.Rotate(ctx, sess); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if info.ID != wantID {
		t.Fatalf("Info.ID = %v after Rotate, want %v — the session id must survive rotation", info.ID, wantID)
	}
	if info.UserID != wantUser {
		t.Fatalf("Info.UserID = %q after Rotate, want %q", info.UserID, wantUser)
	}
	if !info.ExpiresAt.Equal(sess.ExpiresAt()) {
		t.Fatalf("Info.ExpiresAt = %v after Rotate, want %v to match the record — Info must not go stale", info.ExpiresAt, sess.ExpiresAt())
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
