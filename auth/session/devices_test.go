package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
)

// bareStore implements only Store — no UserIndex, no Expirer.
type bareStore struct{ inner *session.MemoryStore }

func (b bareStore) Load(ctx context.Context, tok string) (session.Record, error) {
	return b.inner.Load(ctx, tok)
}
func (b bareStore) Save(ctx context.Context, tok string, r session.Record) (string, error) {
	return b.inner.Save(ctx, tok, r)
}
func (b bareStore) Delete(ctx context.Context, tok string) error { return b.inner.Delete(ctx, tok) }

func TestMissingCapabilityIsErrUnsupported(t *testing.T) {
	mgr, err := session.New(session.DefaultConfig(),
		session.WithStore(bareStore{session.NewMemoryStore()}),
		session.WithTouch(0),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := mgr.ListByUser(t.Context(), "u1"); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("ListByUser = %v, want ErrUnsupported", err)
	}
	if err := mgr.Revoke(t.Context(), "u1", id.NewUUID()); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("Revoke = %v, want ErrUnsupported", err)
	}
	if _, err := mgr.DeleteExpired(t.Context()); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("DeleteExpired = %v, want ErrUnsupported", err)
	}
}

func TestLogoutOthersKeepsTheCurrentSession(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	current := mgr.Start()
	if err := mgr.Authenticate(ctx, current, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	for range 3 {
		other := mgr.Start()
		if err := mgr.Authenticate(ctx, other, "u1"); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}

	list, err := mgr.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("expected 4 sessions before logout-others, got %d", len(list))
	}

	if err := mgr.LogoutOthers(ctx, current); err != nil {
		t.Fatalf("LogoutOthers: %v", err)
	}

	list, err = mgr.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != current.ID() {
		t.Fatalf("LogoutOthers left %d sessions, want only the current one", len(list))
	}
	if _, err := mgr.Load(ctx, current.Token()); err != nil {
		t.Fatalf("the current session must still load after LogoutOthers: %v", err)
	}
}

func TestRevokeIsUserBound(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	victim := mgr.Start()
	if err := mgr.Authenticate(ctx, victim, "victim"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// An attacker knows the victim's session id and tries to revoke it as themselves.
	if err := mgr.Revoke(ctx, "attacker", victim.ID()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := mgr.Load(ctx, victim.Token()); err != nil {
		t.Fatalf("Revoke must refuse a session belonging to another user: %v", err)
	}

	// The owner can revoke it.
	if err := mgr.Revoke(ctx, "victim", victim.ID()); err != nil {
		t.Fatalf("Revoke by owner: %v", err)
	}
	if _, err := mgr.Load(ctx, victim.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("the owner's revoke must remove the session")
	}
}

func TestReaperServiceName(t *testing.T) {
	mgr := newTestManager(t)
	svc := session.Reaper(mgr, time.Minute)
	if svc.Name() == "" {
		t.Fatal("a supervisor.Service must report a name")
	}
}

func TestReaperStopsOnContextCancel(t *testing.T) {
	mgr := newTestManager(t)
	svc := session.Reaper(mgr, time.Hour)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reaper did not stop within 2s of cancellation")
	}
}
