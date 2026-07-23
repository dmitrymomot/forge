package session_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

func TestElevatedWithin(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(now)

	mgr := newTestManager(t, session.WithClock(clk))
	sess := mustStart(t, mgr)

	if sess.ElevatedWithin(time.Minute) {
		t.Fatal("a fresh anonymous session must not be elevated")
	}

	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !sess.ElevatedWithin(time.Minute) {
		t.Fatal("Authenticate must stamp ElevatedAt")
	}

	clk.Advance(2 * time.Minute)
	if sess.ElevatedWithin(time.Minute) {
		t.Fatal("elevation must expire once the window has passed")
	}
	if !sess.ElevatedWithin(5 * time.Minute) {
		t.Fatal("a wider window must still accept the same stamp")
	}
}

func TestAnonymousSessionIsNewAndEmpty(t *testing.T) {
	mgr := newTestManager(t)
	sess := mustStart(t, mgr)

	if !sess.IsNew() {
		t.Fatal("a session that has never been saved must report IsNew")
	}
	if sess.UserID() != "" {
		t.Fatalf("UserID() = %q, want empty for anonymous", sess.UserID())
	}
	if sess.ID().IsZero() {
		t.Fatal("a session must carry an ID before it is saved")
	}
}

func newTestManager(t *testing.T, opts ...session.Option) *session.Manager {
	t.Helper()
	base := []session.Option{session.WithStore(session.NewMemoryStore())}
	mgr, err := session.New(session.DefaultConfig(), append(base, opts...)...)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	return mgr
}

func mustStart(t *testing.T, mgr *session.Manager) *session.Session {
	t.Helper()
	return mgr.Start()
}
